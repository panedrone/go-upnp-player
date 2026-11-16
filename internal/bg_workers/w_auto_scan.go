package bg_workers

import (
	"context"
	"time"

	"github.com/panedrone/go-upnp-player/internal/player"
)

// RunAutoScan searches for devices once a second while the Output card's Scan switch is on.
func RunAutoScan(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if player.AutoScanOn() {
				player.AutoScanStep()
			}
		}
	}
}
