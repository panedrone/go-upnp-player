package player

import (
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/upnp"
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
	notify()

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
	nextURL = ""
	if wasPaused && old.UDN != udn {
		// the position must survive the switch: Play continues it on the new renderer
		resumeAt = pos
	}
	mu.Unlock()
	_ = config.Update(func(c *config.Config) { c.Renderer = udn })
	if hadOld && old.UDN != udn {
		c, cancel := ctx()
		_ = upnp.Pause(c, old) // not Stop, see Stop()
		cancel()
	}
	refreshVolume()
	if was {
		if err := startLocked(currentIndex(), pos); err != nil {
			setErr(err)
			return err
		}
	}
	notify()
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
	notify()
}
