// Package bg_workers schedules background polling.
package bg_workers

import (
	"context"
	"time"

	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/power"
)

// RunPlayerPoll polls the renderer once a second and, while music plays, keeps the PC
// from sleeping; once a minute, if the selected renderer is not found, it rediscovers
// (the streamer was turned on after the program).
func RunPlayerPoll(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	rediscover := time.NewTicker(time.Minute)
	defer rediscover.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			player.Tick()
			if keepAwake(player.Snapshot()) {
				power.StayAwake()
			}
		case <-rediscover.C:
			if selectedMissing(player.Snapshot()) {
				player.Discover()
			}
		}
	}
}

func selectedMissing(s player.State) bool {
	for _, r := range s.Renderers {
		if r.UDN == s.Selected {
			return false
		}
	}
	return true
}

// keepAwake: the player is playing or still starting a track (a refused start being
// retried counts); paused, stopped or played out lets the PC sleep.
func keepAwake(s player.State) bool {
	return s.WantPlaying && !s.Paused
}
