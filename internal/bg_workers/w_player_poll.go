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
			s := player.Snapshot()
			found := false
			for _, r := range s.Renderers {
				found = found || r.UDN == s.Selected
			}
			if !found {
				player.Discover()
			}
		}
	}
}
