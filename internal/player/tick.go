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
	ti, err := getTransportInfo(c, d)
	if err != nil {
		mu.Lock()
		transport = "OFFLINE"
		stalled = false
		mu.Unlock()
		return
	}
	pi, piErr := getPositionInfo(c, d)

	mu.Lock()
	transport = ti.State
	// The renderer counts in its stream: a run of tracks (ADR-016) that may start into
	// its first track (ADR-015). The UI gets the track and the position in it.
	lastStream, lastIndex, lastPos := streamPos, index, position
	now := time.Now()
	if piErr == nil && !seekAt.IsZero() {
		if services.SeekLanded(seekTo, pi.Position, now.Sub(seekAt)) {
			seekAt = time.Time{}
		} else {
			// where it was before the seek: the stream is where it was seeked to
			pi.Position = streamPos
		}
	}
	if piErr == nil {
		// One answer that does not follow from the last (gmrender's odd 0:00:00) is set
		// aside: taken at its word, it looked like a stream that strayed from the
		// playlist, and the player seeked to the next track, with a click on the coaxial
		// output (2026-11-14). The same answer on the next poll is the truth. A seek, a
		// new stream or a fresh command move the position for real and are not doubted.
		settled := seekAt.IsZero() && time.Since(lastCmd) > 3*time.Second &&
			pi.TrackURI != "" && pi.TrackURI == lastTrackURI && !lastPollAt.IsZero()
		if settled && wantPlaying && !paused && !services.PlausibleStep(lastStream, pi.Position) && oddPolls == 0 {
			oddPolls++
			log.Printf("player: renderer reported %.1f s in the stream right after %.1f s; not acted on unless it says so again", pi.Position, lastStream)
			pi.Position = lastStream
		} else {
			oddPolls = 0
		}
		streamPos = pi.Position
	}
	streamDur := runLen
	if pi.Duration > 0 {
		streamDur = pi.Duration
	}
	if piErr != nil || streamPos != lastStream || frozenSince.IsZero() {
		frozenSince = now
	}
	stalled = piErr == nil && services.Stalled(ti.State, now.Sub(frozenSince), streamPos, streamDur)
	racing := false
	if piErr == nil && pi.TrackURI != "" && pi.TrackURI == lastTrackURI && wantPlaying && !paused {
		if services.Racing(ti.State, streamPos-lastStream, now.Sub(lastPollAt)) {
			if racingTicks == 0 {
				racingIndex, racingFrom = lastIndex, lastPos
			}
			racingTicks++
			racing = racingTicks >= services.RacingTicks
		} else {
			racingTicks = 0
		}
	} // a stream change says nothing either way: keep the run as it is
	if piErr == nil {
		lastTrackURI, lastPollAt = pi.TrackURI, now
	}
	nextAt := nextStartLocked()
	curDur := runLen
	if len(runIDs) == 0 {
		curDur = trackDuration(index) - offset // nothing started here: the track as it was
	}
	in := services.TickInput{
		WantPlaying:  wantPlaying && !paused,
		State:        ti.State,
		SinceCmd:     time.Since(lastCmd),
		TrackURI:     pi.TrackURI,
		NextURL:      nextURL,
		HasNext:      nextAt < len(queue),
		Duration:     pi.Duration,
		Position:     pi.Position,
		LastPosition: lastStream,
		CurDuration:  curDur,
		NextDuration: nextRun.length,
	}
	action := services.DecideTick(in)
	moved, strayed := false, false
	if action == services.TickAdvanced {
		setRunLocked(nextRun) // the queued run is the current one now
		streamPos = in.Position
		lastCmd = now
		moved = true
	}
	if piErr == nil && len(runIDs) > 0 && wantPlaying {
		k, pos := locateLocked(streamPos, index-runQ)
		if qi := queueIndexLocked(k); qi != index {
			// the stream went on into its next track; an edited playlist may not have
			// that one next
			moved = true
			strayed = action != services.TickAdvanced && (qi < 0 || qi != lastIndex+1)
			if !strayed {
				index = qi
			}
		}
		if !strayed {
			position, duration = pos, runDurs[k]
		}
	}
	tickCount++
	poll := tickCount%10 == 0
	mu.Unlock()

	if racing {
		// its position and track mean nothing now: no queue decisions on them
		recoverRacing(d)
		return
	}
	if moved && !strayed {
		savePlaylist()
	}
	if strayed && action == services.TickNone {
		// the playlist was edited: its next track is not the one the stream holds
		if lastIndex+1 < len(queue) {
			setErr(startLocked(lastIndex+1, 0))
		} else {
			mu.Lock()
			wantPlaying = false
			mu.Unlock()
			_ = halt(c, d)
		}
		return
	}
	switch action {
	case services.TickAdvanced:
		queueNext(d)
	case services.TickPlayNext:
		if err := startLocked(nextStart(), 0); err != nil {
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

// nextStartLocked is the queue index the run after the current one starts at: right
// after the run, or after the current track when there is no run (yet).
func nextStartLocked() int {
	if len(runIDs) > 0 && runCoherentLocked() {
		return runQ + len(runIDs)
	}
	return index + 1
}

func nextStart() int {
	mu.Lock()
	defer mu.Unlock()
	return nextStartLocked()
}
