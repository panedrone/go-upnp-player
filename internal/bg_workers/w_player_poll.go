// Package bg_workers schedules background polling.
package bg_workers

import (
	"context"
	"time"

	"github.com/panedrone/go-upnp-player/internal/player"
)

// RunPlayerPoll polls the renderer once a second; once a minute, if the selected
// renderer is not found, it rediscovers (the streamer was turned on after the program).
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
