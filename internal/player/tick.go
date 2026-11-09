package player

import (
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
		mu.Unlock()
		notify()
		return
	}
	pi, _ := upnp.GetPositionInfo(c, d)

	mu.Lock()
	transport = ti.State
	if pi.Duration > 0 {
		duration = pi.Duration
	}
	lastPos := position
	position = pi.Position
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
