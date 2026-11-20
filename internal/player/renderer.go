package player

import (
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/upnp"
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
		ok := false
		for _, d := range found {
			ok = ok || d.UDN == selected
		}
		if !ok && selected != "" {
			// the selected renderer stays on the list while it is silent: it shows OFFLINE
			// instead of vanishing (Valera rebuilding itself after its card left, ADR-032)
			for _, d := range renderers {
				if d.UDN == selected {
					found = append(found, d)
					sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
					break
				}
			}
		}
		renderers = found
		if !ok && len(found) > 0 && selected == "" {
			selected = found[0].UDN
		}
	}
	mu.Unlock()
	setErr(err)
	refreshVolume()
}

// SetAutoScan turns the Output card's Scan switch on or off. While on, AutoScanStep runs
// every second (bg_workers.RunAutoScan) until it is turned off: a renderer may take minutes to
// boot, so it never turns itself off (the owner, 2026-11-16).
func SetAutoScan(on bool) {
	mu.Lock()
	autoScan, scanSeen = on, map[string]time.Time{}
	mu.Unlock()
}

// AutoScanOn reports whether the Scan switch is on.
func AutoScanOn() bool {
	mu.Lock()
	defer mu.Unlock()
	return autoScan
}

// AutoScanStep searches for devices with a one-second wait, adds the new ones to the list and
// drops those it has not heard for services.ScanGone (a device unplugged; one lost answer does
// not drop it). Known devices are not asked for their descriptions again.
func AutoScanStep() {
	mu.Lock()
	if !autoScan || discovering {
		mu.Unlock()
		return
	}
	known := append([]upnp.Device(nil), renderers...)
	mu.Unlock()

	c, cancel := ctx()
	defer cancel()
	found, err := rediscoverRenderers(c, time.Second, known)

	mu.Lock()
	defer mu.Unlock()
	if err != nil || !autoScan {
		return
	}
	now := time.Now()
	for _, d := range found {
		scanSeen[d.UDN] = now
		// a device back at another address (a renderer restarted on a new port) is taken
		// as it answers now: the old address would leave it OFFLINE for good
		for i := range renderers {
			if renderers[i].UDN == d.UDN {
				renderers[i] = d
			}
		}
	}
	added := services.NewUDNs(udns(renderers), udns(found))
	for _, d := range found {
		for _, u := range added {
			if d.UDN == u {
				renderers = append(renderers, d)
			}
		}
	}
	// a device that came onto the list some other way (Refresh) counts from now
	for _, d := range renderers {
		if _, ok := scanSeen[d.UDN]; !ok {
			scanSeen[d.UDN] = now
		}
	}
	gone := services.GoneUDNs(udns(renderers), scanSeen, now)
	kept := renderers[:0]
	for _, d := range renderers {
		// the selected renderer is never dropped: silent, it is shown OFFLINE until it is
		// back, the way a FiiO SR11 with its amplifier on another input is (ADR-032)
		if d.UDN == selected || !slices.Contains(gone, d.UDN) {
			kept = append(kept, d)
		}
	}
	for _, u := range gone {
		if u != selected {
			delete(scanSeen, u)
		}
	}
	renderers = kept
	if len(added) > 0 {
		sort.Slice(renderers, func(i, j int) bool { return renderers[i].Name < renderers[j].Name })
		if selected == "" {
			selected = renderers[0].UDN
		}
	}
}

func udns(list []upnp.Device) []string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = d.UDN
	}
	return out
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
	frozenSince, stalled, wentOffline = time.Time{}, false, false
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
