package player

import (
	"context"
	"testing"
	"time"

	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

// Scan adds what a search finds, keeps a device that missed a second, drops one not heard for
// services.ScanGone, does not ask a known device again, and runs until it is turned off.
func TestAutoScanStep(t *testing.T) {
	resetState()
	t.Cleanup(resetState)
	var answer []upnp.Device
	var gotKnown []upnp.Device
	old := rediscoverRenderers
	rediscoverRenderers = func(_ context.Context, _ time.Duration, known []upnp.Device) ([]upnp.Device, error) {
		gotKnown = known
		return answer, nil
	}
	t.Cleanup(func() { rediscoverRenderers = old })

	a, b := upnp.Device{UDN: "a", Name: "Alpha"}, upnp.Device{UDN: "b", Name: "Beta"}
	renderers = []upnp.Device{b}
	SetAutoScan(true)

	answer = []upnp.Device{a} // b missed the second
	AutoScanStep()
	if s := Snapshot(); len(s.Renderers) != 2 || s.Renderers[0].UDN != "a" || s.Renderers[1].UDN != "b" {
		t.Fatalf("renderers %+v, want a then b", s.Renderers)
	}
	if len(gotKnown) != 1 || gotKnown[0].UDN != "b" {
		t.Errorf("known passed to the search: %+v", gotKnown)
	}
	if Snapshot().Selected != "a" {
		t.Errorf("nothing was selected: the first found is taken")
	}

	// nothing heard for a while: still on, nothing dropped yet
	answer = nil
	AutoScanStep()
	if !AutoScanOn() || !Snapshot().AutoScan {
		t.Fatal("turned itself off")
	}
	if len(Snapshot().Renderers) != 2 {
		t.Error("a device was dropped at once")
	}
	// b unheard for ScanGone, a heard now: b goes
	mu.Lock()
	scanSeen["b"] = time.Now().Add(-services.ScanGone)
	mu.Unlock()
	answer = []upnp.Device{a}
	AutoScanStep()
	if s := Snapshot(); len(s.Renderers) != 1 || s.Renderers[0].UDN != "a" {
		t.Fatalf("renderers %+v, want only a", s.Renderers)
	}
	SetAutoScan(false)
	if AutoScanOn() {
		t.Error("still on after it was turned off")
	}
}
