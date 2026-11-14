package player

import (
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/upnp"
	"sync"
	"time"
)

// Discover finds renderers; if the selected one is absent, it takes the first one found.
func Discover() {
	mu.Lock()
	if discovering {
		mu.Unlock()
		return
	}
	discovering = true
	mu.Unlock()

	c, cancel := ctx()
	defer cancel()
	found, err := DiscoverRenderers(c, 3*time.Second)

	mu.Lock()
	discovering = false
	if err == nil {
		renderers = found
		ok := false
		for _, d := range found {
			ok = ok || d.UDN == selected
		}
		if !ok && len(found) > 0 && selected == "" {
			selected = found[0].UDN
		}
	}
	mu.Unlock()
	setErr(err)
	refreshVolume()
}

// halting counts the old renderers still being halted after a switch (for tests).
var halting sync.WaitGroup

// Select switches the renderer. If playing, the current track continues on the new one.
func Select(udn string) error {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	old, hadOld := device()
	mu.Lock()
	was := wantPlaying && !paused
	wasPaused := wantPlaying && paused
	pos := position
	selected = udn
	// the new renderer's state is unknown until it is polled: the badge must not carry
	// the old one's (a red OFFLINE moved from the BeagleBone to the FiiO, 2026-11-10)
	transport = ""
	frozenSince, stalled = time.Time{}, false
	lastTrackURI, racingTicks, racingFixAt = "", 0, time.Time{}
	pendingSince = time.Time{} // a refused start belonged to the old renderer
	nextURL, nextRun = "", runPlan{}
	if wasPaused && old.UDN != udn {
		// the position must survive the switch: Play continues it on the new renderer
		resumeAt = pos
	}
	mu.Unlock()
	_ = config.Update(func(c *config.Config) { c.Renderer = udn })
	// The old renderer is halted alongside the start on the new one, not before it:
	// a renderer that does not answer held the switch for the whole request timeout
	// (BeagleBone answering 400 Bad Request, 2026-11-10).
	if hadOld && old.UDN != udn {
		halting.Add(1)
		go func() {
			defer halting.Done()
			c, cancel := ctx()
			defer cancel()
			_ = stopRenderer(c, old)
		}()
	}
	refreshVolume()
	if was {
		if err := startLocked(currentIndex(), pos); err != nil {
			setErr(err)
			return err
		}
	}
	return nil
}

func refreshVolume() {
	d, ok := device()
	if !ok {
		return
	}
	c, cancel := ctx()
	defer cancel()
	v, err := upnp.GetVolume(c, d)
	if err != nil {
		return
	}
	mu.Lock()
	volume = v
	mu.Unlock()
}
