//go:build windows

package window

import (
	"testing"

	"github.com/panedrone/go-upnp-player/internal/config"
)

func TestUsable(t *testing.T) {
	if !usable(config.Window{Left: 100, Top: 50, Right: 1460, Bottom: 950}) {
		t.Error("a normal window")
	}
	if usable(config.Window{}) || usable(config.Window{Left: -32000, Top: -32000, Right: -31840, Bottom: -31972}) {
		t.Error("an empty or a minimized-sized rectangle")
	}
}
