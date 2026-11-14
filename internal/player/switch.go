package player

import (
	"errors"
	"fmt"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/upnp"
	"log"
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
	rp, err := planStart(d, i, from)
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
	ti, _ := getTransportInfo(c, d)
	pi, _ := getPositionInfo(c, d)
	plan := services.PlanSwitch(ti.State, pi.Duration)
	if plan.ViaNext && usesStop(d.Model) {
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
	restarted := false
	if plan.ViaNext && pi.TrackURI == uri && services.SameLength(pi.Duration, rp.length) {
		restarted = true
		// the renderer already has this very stream: restart it in place. The URI
		// alone is not proof — gmrender reports the queued next URI early — so the
		// audible length must match too.
		sent := time.Now()
		if err := seekRenderer(c, d, rp.lead); err != nil {
			return err
		}
		if plan.Resume {
			if !confirmAt(d, rp.lead, sent) {
				log.Printf("player: %s did not report the position %.0f s; playing anyway", d.Name, rp.lead)
			}
			if err := playRetried(d); err != nil {
				return err
			}
			if !confirm(d, func(st renderState) bool { return st.state == "PLAYING" }) {
				log.Printf("player: %s did not report itself playing", d.Name)
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
	if rp.lead > 0 && !restarted {
		seekLead(d, uri, rp.length, rp.lead)
	}
	mu.Lock()
	resumeAt = -1
	pendingSince = time.Time{}
	index, wantPlaying, paused = i, true, false
	lastCmd = time.Now()
	setRunLocked(rp)
	if restarted || rp.lead > 0 {
		// seeked: the polls may see the old position for a while
		seekSentLocked(rp.lead)
	}
	position, duration = rp.from, t.Duration
	if rp.lead > 0 {
		position = from
	}
	frozenSince, stalled = time.Now(), false
	lastTrackURI, racingTicks = "", 0 // the first poll after a start is the new baseline
	mu.Unlock()
	savePlaylist()
	queueNext(d)
	return nil
}

// seekLead takes a fresh stream past its silent lead to the chosen track (ADR-020).
// gmrender drops a seek sent in the first moments of a stream, and right after a start
// it still reports the previous stream's position (ADR-005): the first try, 2026-11-14,
// took that for a landed seek, and the renderer played the silent lead. So the seek goes
// once the renderer reports the new stream playing, and it is landed only when the new
// stream stands at the chosen place; it is repeated until then. Meanwhile the lead
// plays silence, never the tracks before the chosen one.
func seekLead(d upnp.Device, uri string, length, at float64) {
	sent := 0
	var sentAt time.Time
	last := -1.0 // the position at the previous poll
	for range 40 {
		sleep(300 * time.Millisecond)
		c, cancel := ctx()
		pi, err := getPositionInfo(c, d)
		cancel()
		// gmrender reports a queued URI early, so its length must match too
		if err != nil || pi.TrackURI != uri || !services.SameLength(pi.Duration, length) {
			last = -1
			continue // still the old stream
		}
		if sent > 0 && pi.Position >= at-2 {
			return // landed
		}
		if sent == 0 {
			// The seek waits for the new stream to settle: the renderer reports it
			// playing, more than a second in, its position going on from the previous
			// poll. A seek while the FiiO was still starting the stream made it stutter
			// at every switch (1.1.20, 2026-11-14).
			settled := last >= 1 && pi.Position > last
			last = pi.Position
			if !settled {
				continue
			}
		}
		if sent > 0 {
			// A seek is out: wait for it. The FiiO goes on reporting its old position for
			// a while after a seek, and a seek repeated on every such report restarted its
			// buffer again and again: a stutter at every switch (1.1.20, 2026-11-14).
			// Another seek only when, 3 s on, the renderer is clearly still playing the
			// lead, its position moving on from where it was.
			moving := last >= 0 && pi.Position > last
			last = pi.Position
			if time.Since(sentAt) < 3*time.Second || !moving {
				continue
			}
			if sent >= 4 {
				break
			}
			log.Printf("player: the seek past the silent lead to %.0f s did not land (the renderer is at %.0f s): once more", at, pi.Position)
		}
		c, cancel = ctx()
		err = seekRenderer(c, d, at)
		cancel()
		if err == nil {
			sent++
			sentAt = time.Now()
		}
	}
	log.Printf("player: the seek past the silent lead to %.0f s did not land", at)
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
		ti, err := getTransportInfo(c, d)
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
		pi, err := getPositionInfo(c, d)
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
		ti, _ := getTransportInfo(c, d)
		pi, _ := getPositionInfo(c, d)
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
	if err := seekRenderer(c, d, plan.SeekTo); err != nil {
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
