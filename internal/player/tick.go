package player

import (
	"fmt"
	"log"

	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/upnp"
	"time"
)

// Tick is one renderer poll: position, state, queue advance.
// Called by the worker once a second.
func Tick() {
	if !cmdMu.TryLock() {
		return // a command is in progress; poll next time
	}
	defer cmdMu.Unlock()
	d, ok := device()
	if !ok {
		return
	}
	if retryPendingStart(d) {
		return // nothing to poll while a refused start is being retried
	}
	c, cancel := ctx()
	defer cancel()
	ti, err := upnp.GetTransportInfo(c, d)
	if err != nil {
		mu.Lock()
		transport = "OFFLINE"
		stalled = false
		mu.Unlock()
		return
	}
	pi, piErr := upnp.GetPositionInfo(c, d)

	mu.Lock()
	transport = ti.State
	// the renderer counts from the start of its stream; a stream that starts at a
	// position (ADR-015) is shifted by offset against the track
	off := offset
	if pi.Duration > 0 {
		duration = pi.Duration + off
	}
	lastPos := position
	abs := pi.Position + off
	position = abs
	now := time.Now()
	if piErr != nil || abs != lastPos || frozenSince.IsZero() {
		frozenSince = now
	}
	stalled = piErr == nil && services.Stalled(ti.State, now.Sub(frozenSince), abs, duration)
	racing := false
	if piErr == nil && pi.TrackURI != "" && pi.TrackURI == lastTrackURI && wantPlaying && !paused {
		if services.Racing(ti.State, abs-lastPos, now.Sub(lastPollAt)) {
			if racingTicks == 0 {
				racingIndex, racingFrom = index, lastPos
			}
			racingTicks++
			racing = racingTicks >= services.RacingTicks
		} else {
			racingTicks = 0
		}
	} // a track change says nothing either way: keep the run as it is
	if piErr == nil {
		lastTrackURI, lastPollAt = pi.TrackURI, now
	}
	in := services.TickInput{
		WantPlaying: wantPlaying && !paused,
		State:       ti.State,
		SinceCmd:    time.Since(lastCmd),
		TrackURI:    pi.TrackURI,
		NextURL:     nextURL,
		HasNext:     index+1 < len(queue),
		// the advance decision works on the renderer's own figures: the current
		// track's stream is shorter by offset, the next one starts at 0
		Duration:     pi.Duration,
		Position:     pi.Position,
		LastPosition: lastPos - off,
		CurDuration:  trackDuration(index) - off,
		NextDuration: trackDuration(index + 1),
	}
	tickCount++
	poll := tickCount%10 == 0
	mu.Unlock()

	if racing {
		// its position and track mean nothing now: no queue decisions on them
		recoverRacing(d)
		return
	}
	switch services.DecideTick(in) {
	case services.TickAdvanced:
		mu.Lock()
		index++
		nextURL = ""
		offset = 0 // the next track's stream starts at its beginning
		position, duration = in.Position, in.Duration
		lastCmd = time.Now()
		mu.Unlock()
		savePlaylist()
		queueNext(d)
	case services.TickPlayNext:
		if err := startLocked(currentIndex()+1, 0); err != nil {
			// Do not retry on every poll: a BeagleBone answering every Play with
			// "501 Playing failed" got 108 of them in two minutes (2026-11-09).
			mu.Lock()
			wantPlaying = false
			mu.Unlock()
			setErr(err)
		}
	case services.TickFinished:
		mu.Lock()
		wantPlaying = false
		if ti.State == "PLAYING" {
			finishedAt = time.Now()
		}
		mu.Unlock()
	}
	mu.Lock()
	haltNow := !finishedAt.IsZero() && time.Since(finishedAt) >= 2*time.Second
	if haltNow || wantPlaying {
		finishedAt = time.Time{}
	}
	mu.Unlock()
	if haltNow && ti.State == "PLAYING" {
		// It sits in PLAYING at the end of the last track (the FiiO SR11): out of play,
		// so its state stops saying PLAYING over silence. A moment after the end, not at
		// it: the position comes in whole seconds and up to half a second may be left.
		_ = halt(c, d)
	}
	if poll {
		if v, err := upnp.GetVolume(c, d); err == nil {
			mu.Lock()
			volume = v
			mu.Unlock()
		}
	}
}

// retryPendingStart retries a start the renderer refused (ADR-014) and reports
// whether one is pending. After services.StartRetryFor it gives up with an error.
func retryPendingStart(d upnp.Device) bool {
	mu.Lock()
	if pendingSince.IsZero() {
		mu.Unlock()
		return false
	}
	now := time.Now()
	retry, giveUp := services.RetryStart(now.Sub(pendingSince), now.Sub(pendingTry))
	i, pos, tries := pendingIndex, pendingPos, pendingTries+1
	if giveUp {
		pendingSince = time.Time{}
		wantPlaying = false
		if pos > 1 {
			resumeAt = pos // the next Play still starts where it should
		}
	}
	mu.Unlock()
	switch {
	case giveUp:
		setErr(fmt.Errorf("renderer %s refused to play %d times in %s: is its output switched on (the amplifier input)?",
			d.Name, tries, services.StartRetryFor))
	case retry:
		setErr(startLocked(i, pos))
	}
	return true
}

// racingRetry: a renderer that races again this soon after a restart is not cured by
// one, so it is paused and reported instead of being stopped over and over.
const racingRetry = 2 * time.Minute

// recoverRacing restarts the track the way foobar2000 does: Stop, SetAVTransportURI,
// Play, then the position where the racing began. Stop is otherwise never sent (it hangs
// the FiiO SR11, see Stop()); here the renderer plays nothing anyway. Call under cmdMu.
func recoverRacing(d upnp.Device) {
	mu.Lock()
	i, pos := racingIndex, racingFrom
	again := !racingFixAt.IsZero() && time.Since(racingFixAt) < racingRetry
	racingTicks = 0
	racingFixAt = time.Now()
	mu.Unlock()
	c, cancel := ctx()
	defer cancel()
	if again {
		_ = upnp.Pause(c, d)
		mu.Lock()
		paused = true
		mu.Unlock()
		setErr(fmt.Errorf("renderer %s runs through tracks with no sound even after a restart: restart the renderer", d.Name))
		return
	}
	// a fault worth seeing in the log: the cause on the renderer side is still unknown
	log.Printf("player: renderer %s plays ahead of the clock with no sound: restarting queue track %d at %.0f s", d.Name, i, pos)
	if err := upnp.Stop(c, d); err != nil {
		setErr(err)
		return
	}
	setErr(startLocked(i, pos))
}
