package player

import (
	"fmt"
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
	c, cancel := ctx()
	defer cancel()
	ti, err := upnp.GetTransportInfo(c, d)
	if err != nil {
		mu.Lock()
		transport = "OFFLINE"
		stalled = false
		mu.Unlock()
		notify()
		return
	}
	pi, piErr := upnp.GetPositionInfo(c, d)

	mu.Lock()
	transport = ti.State
	if pi.Duration > 0 {
		duration = pi.Duration
	}
	lastPos := position
	position = pi.Position
	now := time.Now()
	if piErr != nil || pi.Position != lastPos || frozenSince.IsZero() {
		frozenSince = now
	}
	stalled = piErr == nil && services.Stalled(ti.State, now.Sub(frozenSince))
	racing := false
	if piErr == nil && pi.TrackURI != "" && pi.TrackURI == lastTrackURI && wantPlaying && !paused {
		if services.Racing(ti.State, pi.Position-lastPos, now.Sub(lastPollAt)) {
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
		WantPlaying:  wantPlaying && !paused,
		State:        ti.State,
		SinceCmd:     time.Since(lastCmd),
		TrackURI:     pi.TrackURI,
		NextURL:      nextURL,
		HasNext:      index+1 < len(queue),
		Duration:     pi.Duration,
		Position:     pi.Position,
		LastPosition: lastPos,
		CurDuration:  trackDuration(index),
		NextDuration: trackDuration(index + 1),
	}
	tickCount++
	poll := tickCount%10 == 0
	mu.Unlock()

	if racing {
		// its position and track mean nothing now: no queue decisions on them
		recoverRacing(d)
		notify()
		return
	}
	switch services.DecideTick(in) {
	case services.TickAdvanced:
		mu.Lock()
		index++
		nextURL = ""
		lastCmd = time.Now()
		mu.Unlock()
		savePlaylist()
		queueNext(d)
	case services.TickPlayNext:
		setErr(startLocked(currentIndex()+1, 0))
	case services.TickFinished:
		mu.Lock()
		wantPlaying = false
		mu.Unlock()
	}
	if poll {
		if v, err := upnp.GetVolume(c, d); err == nil {
			mu.Lock()
			volume = v
			mu.Unlock()
		}
	}
	notify()
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
	if err := upnp.Stop(c, d); err != nil {
		setErr(err)
		return
	}
	setErr(startLocked(i, pos))
}
