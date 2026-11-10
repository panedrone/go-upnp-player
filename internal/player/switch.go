package player

import (
	"errors"
	"fmt"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/upnp"
	"time"
)

// startLocked starts track i of the queue; call it under cmdMu.
func startLocked(i int, seek float64) (err error) {
	defer func() {
		// The renderer took the URI but refused to play: not an error yet. The poll
		// retries the start for a while (ADR-014); the UI says so meanwhile.
		if errors.Is(err, errPlayRefused) {
			mu.Lock()
			now := time.Now()
			if pendingSince.IsZero() {
				pendingSince, pendingTries = now, 0
			} else {
				pendingTries++
			}
			pendingIndex, pendingPos, pendingTry = i, seek, now
			index, wantPlaying, paused, resumeAt = i, true, false, -1
			position, nextURL = seek, ""
			mu.Unlock()
			err = nil
			return
		}
		// A failed start must not lose the position it was asked for: the next Play
		// would otherwise start the current track from 0 (BeagleBone, 2026-11-10).
		if err != nil && seek > 1 {
			mu.Lock()
			if i == index {
				resumeAt = seek
			}
			mu.Unlock()
		}
	}()
	d, ok := device()
	if !ok {
		return fmt.Errorf("no renderer selected")
	}
	mu.Lock()
	if i < 0 || i >= len(queue) {
		mu.Unlock()
		return fmt.Errorf("index %d out of range", i)
	}
	id := queue[i]
	mu.Unlock()
	t := lookupTrack(id)
	if t == nil {
		return fmt.Errorf("track %s is not in the library anymore", id)
	}
	// A start at a position is a stream that begins there: the renderer plays it from
	// its start, with no seek. Play from 0 and a seek afterwards always let the
	// opening of the track be heard for a moment (2026-11-10, ADR-015).
	from := 0.0
	if seek > 1 && !services.AtEnd(seek, t.Duration) {
		from = seek
	}
	// a start at the very end is a start of a track that has already ended: from 0
	rp, err := planRun(d, i, from)
	if err != nil {
		return err
	}
	uri, meta := rp.uri, rp.meta
	c, cancel := ctx()
	defer cancel()

	// How to switch (2026-11-09, both renderers run gmrender):
	//   - gmrender takes a new SetAVTransportURI only from STOPPED; while playing
	//     or paused, Play keeps the old song going;
	//   - Stop is not an option: on the FiiO SR11 it hangs control until a power
	//     cycle, both during playback and from pause.
	// So a busy renderer is switched through its gapless path: the target goes
	// in as the "next" URI and the current track is seeked to its last second;
	// the renderer moves on by itself.
	ti, _ := upnp.GetTransportInfo(c, d)
	pi, _ := upnp.GetPositionInfo(c, d)
	plan := services.PlanSwitch(ti.State, pi.Duration)
	if plan.ViaNext && services.StopSafe(d.Model) {
		// A renderer that survives Stop is started the way foobar2000 starts it:
		// Stop -> SetAVTransportURI -> Play, a fresh pipeline every time. On the
		// BeagleBone the next-URI path with a seek to the end was the suspected
		// trigger of the racing, which foobar2000 never sees there (ADR-007).
		if err := upnp.Stop(c, d); err != nil {
			return err
		}
		waitStopped(d)
		plan = services.SwitchPlan{}
	}
	if plan.ViaNext && pi.TrackURI == uri && services.SameLength(pi.Duration, rp.length) {
		// the renderer already has this very stream: restart it in place. The URI
		// alone is not proof — gmrender reports the queued next URI early — so the
		// audible length must match too.
		if err := upnp.Seek(c, d, 0); err != nil {
			return err
		}
		if plan.Resume {
			if err := playRetried(d); err != nil {
				return err
			}
		}
		plan.ViaNext = false
	} else if plan.ViaNext {
		// The gapless path makes the renderer play the last second of its current
		// stream before the target: the end of the run, another song, or of the track a
		// paused renderer was left on (FiiO, 2026-11-10). The player serves that stream,
		// so it is let go first: what the renderer fetches of it from now on is silence.
		unretire := retireStreams(uri, pi.TrackURI)
		var switched bool
		switched, plan, err = switchViaNextRetried(d, plan, uri, meta, rp.length)
		if err != nil {
			unretire()
			return err
		}
		if !switched && plan.ViaNext {
			unretire()
			// A hung gmrender accepts every command and moves nowhere; this used to be
			// reported as a successful start (BeagleBone, 2026-11-09).
			return fmt.Errorf("renderer %s did not switch to %q: it may be hung, restart it", d.Name, t.Title)
		}
		if !switched { // it stopped in the meantime, so a plain start works now
			if err := startDirect(d, uri, meta); err != nil {
				return err
			}
		}
	} else if err := startDirect(d, uri, meta); err != nil {
		return err
	}
	mu.Lock()
	resumeAt = -1
	pendingSince = time.Time{}
	index, wantPlaying, paused = i, true, false
	lastCmd = time.Now()
	setRunLocked(rp)
	position, duration = rp.from, t.Duration
	frozenSince, stalled = time.Now(), false
	lastTrackURI, racingTicks = "", 0 // the first poll after a start is the new baseline
	mu.Unlock()
	savePlaylist()
	queueNext(d)
	return nil
}

// errPlayRefused: the renderer took the URI but answered every Play with an error.
var errPlayRefused = errors.New("renderer refused to play")

// playTries: the BeagleBone answered a Play sent right after Stop with "501 Playing
// failed" (2026-11-10), most likely while its ALSA device was still being released;
// a moment later the same Play goes through.
const playTries = 4

// startDirect starts a track on a renderer that is not busy.
func startDirect(d upnp.Device, uri, meta string) error {
	c, cancel := ctx()
	defer cancel()
	if err := upnp.SetURI(c, d, uri, meta); err != nil {
		return err
	}
	return playRetried(d)
}

// playRetried sends Play up to playTries times; a renderer that refuses them all
// gives errPlayRefused, which the poll retries for a while (ADR-014).
func playRetried(d upnp.Device) error {
	var err error
	for try := range playTries {
		if try > 0 {
			sleep(700 * time.Millisecond)
		}
		c, cancel := ctx()
		err = upnp.Play(c, d)
		cancel()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: %v", errPlayRefused, err)
}

// waitStopped gives a renderer that was just sent Stop up to 2 s to report STOPPED:
// it tears its pipeline down asynchronously.
func waitStopped(d upnp.Device) {
	for range 10 {
		c, cancel := ctx()
		ti, err := upnp.GetTransportInfo(c, d)
		cancel()
		if err == nil && ti.State == "STOPPED" {
			return
		}
		sleep(200 * time.Millisecond)
	}
}

// waitSwitched waits (up to ~6 s) until the renderer reports the target track's
// length, i.e. the gapless switch has happened. Only then may the next "next"
// URI be queued, otherwise it would replace the target before the switch.
func waitSwitched(d upnp.Device, targetDur float64) bool {
	for range 30 {
		c, cancel := ctx()
		pi, err := upnp.GetPositionInfo(c, d)
		cancel()
		if err == nil && services.SwitchDone(pi.Duration, pi.Position, targetDur) {
			return true
		}
		sleep(200 * time.Millisecond)
	}
	return false
}

// switchViaNextRetried makes up to three attempts: if the renderer had already pulled
// a previously queued "next" into its buffer, our SetNext comes too late and it lands
// on that track instead (seen live 2026-11-09). The wrong track is then the current
// one and the next attempt goes through. It returns the plan for the renderer's
// latest state: one that no longer goes via next means the renderer stopped.
func switchViaNextRetried(d upnp.Device, plan services.SwitchPlan, uri, meta string, targetDur float64) (bool, services.SwitchPlan, error) {
	for attempt := 0; attempt < 3 && plan.ViaNext; attempt++ {
		if err := switchViaNext(d, plan, uri, meta); err != nil {
			return false, plan, err
		}
		if waitSwitched(d, targetDur) {
			return true, plan, nil
		}
		c, cancel := ctx()
		ti, _ := upnp.GetTransportInfo(c, d)
		pi, _ := upnp.GetPositionInfo(c, d)
		cancel()
		plan = services.PlanSwitch(ti.State, pi.Duration)
	}
	return false, plan, nil
}

// switchViaNext: the target as the "next" URI, the current track seeked to its
// last second, Play if paused — the renderer moves on by itself.
func switchViaNext(d upnp.Device, plan services.SwitchPlan, uri, meta string) error {
	c, cancel := ctx()
	defer cancel()
	if err := upnp.SetNextURI(c, d, uri, meta); err != nil {
		return err
	}
	if err := upnp.Seek(c, d, plan.SeekTo); err != nil {
		return err
	}
	if plan.Resume {
		return upnp.Play(c, d)
	}
	return nil
}

// queueNext queues the run after the current one via SetNextAVTransportURI (gapless
// between runs: they change only where the format changes). If unsupported, no harm
// done: DecideTick starts the next run itself.
func queueNext(d upnp.Device) {
	mu.Lock()
	ni := runQ + len(runIDs)
	ok := len(runIDs) > 0 && runCoherentLocked() && ni < len(queue)
	mu.Unlock()
	if !ok {
		return
	}
	rp, err := planRun(d, ni, 0)
	if err != nil {
		return
	}
	c, cancel := ctx()
	defer cancel()
	if err := upnp.SetNextURI(c, d, rp.uri, rp.meta); err != nil {
		return
	}
	mu.Lock()
	nextURL, nextRun = rp.uri, rp
	mu.Unlock()
}
