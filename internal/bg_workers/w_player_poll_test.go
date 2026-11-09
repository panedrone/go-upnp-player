package bg_workers

import (
	"context"
	"testing"
	"time"

	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

func TestSelectedMissing(t *testing.T) {
	list := []upnp.Device{{UDN: "a"}, {UDN: "b"}}
	if selectedMissing(player.State{Renderers: list, Selected: "b"}) {
		t.Fatal("b is on the list")
	}
	if !selectedMissing(player.State{Renderers: list, Selected: "c"}) {
		t.Fatal("c is not on the list")
	}
	if !selectedMissing(player.State{}) {
		t.Fatal("nothing found yet: keep looking")
	}
}

func TestRunPlayerPollStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunPlayerPoll(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the worker did not stop")
	}
}
