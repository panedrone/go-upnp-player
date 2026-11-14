package player

import (
	"errors"
	"fmt"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
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
	c, cancel := ctx()
	defer cancel()

	// gmrender takes a new SetAVTransportURI only from STOPPED; while playing or paused,
	// Play keeps the old song going (2026-11-09).
	ti, _ := getTransportInfo(c, d)
	pi, _ := getPositionInfo(c, d)
	plan := services.PlanSwitch(ti.State, pi.Duration)
	// What the renderer holds decides the start. Holding the player's current stream with
	// the track in it, it is restarted there in place. Holding nothing (STOPPED) or
	// another stream, it gets a stream that begins at the track: no seek. A start from
	// STOPPED that opened the album's stream and seeked into it jumped audibly before it
	// played (the FiiO SR11, 2026-11-14).
	rp, held := heldRun(plan.ViaNext, pi, i, from)
	if !held && plan.ViaNext {
		// A new stream on a busy renderer goes the way foobar2000 starts it: Stop ->
		// SetAVTransportURI -> Play, on every renderer alike (ADR-025). The gapless
		// path (the target as the next URI, a seek to the end) left the BeagleBone
		// hung three times in a row (2026-11-14).
		if err := stopRenderer(c, d); err != nil {
			return err
		}
		plan = services.SwitchPlan{}
	}
	if !held {
		var err error
		if rp, err = planRun(d, i, from); err != nil {
			return err
		}
	}
	uri, meta := rp.uri, rp.meta
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
			if !confirmAt(d, uri, rp.lead, sent) {
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
	} else if err := startDirect(d, uri, meta); err != nil {
		return err
	}
	mu.Lock()
	resumeAt = -1
	pendingSince = time.Time{}
	index, wantPlaying, paused, off = i, true, false, false
	lastCmd = time.Now()
	setRunLocked(rp)
	if restarted {
		// seeked: the polls may see the old position for a while
		seekSentLocked(rp.lead)
	}
	position, duration = rp.from, t.Duration
	if held {
		position = from
	}
	frozenSince, stalled = time.Now(), false
	lastTrackURI, racingTicks = "", 0 // the first poll after a start is the new baseline
	mu.Unlock()
	savePlaylist()
	queueNext(d)
	return nil
}

// heldRun is the player's current run with queue track i, from seconds into it, as the
// place to restart at (lead), when the renderer holds that run: busy (viaNext) and
// reporting its URI and length. Otherwise false.
func heldRun(viaNext bool, pi upnp.PositionInfo, i int, from float64) (runPlan, bool) {
	mu.Lock()
	defer mu.Unlock()
	if !viaNext || runURI == "" || pi.TrackURI != runURI || !services.SameLength(pi.Duration, runLen) {
		return runPlan{}, false
	}
	at, ok := streamTimeLocked(i, from)
	if !ok {
		return runPlan{}, false
	}
	rp := curRun
	rp.lead = at
	if id, ok := runIDOf(rp.uri); ok {
		stream.KeepRun(id) // Stop let it go to silence: music again
	}
	return rp, true
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
