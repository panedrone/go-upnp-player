package player

import (
	"context"
	"testing"
	"time"

	"github.com/panedrone/go-upnp-player/internal/testkit"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

// Valera rebuilt after its card left the bus: OFFLINE for a while, then back with nothing in
// it. The player stops where the music was cut and sends nothing; the next track is not
// started into a card that is not there (ADR-032). Play continues at the place.
func TestBackFromOfflineEmptyStopsInPlace(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "b"}
	playingAt(r, queue, 0, 100)
	Tick()
	r.Fail("GetTransportInfo", true)
	Tick()
	if transport != "OFFLINE" {
		t.Fatalf("transport %q", transport)
	}
	r.Fail("GetTransportInfo", false)
	r.With(func(s *testkit.RendererState) { s.State, s.URI, s.Pos = "NO_MEDIA_PRESENT", "", 0 })
	before := len(r.Actions())
	Tick()
	if wantPlaying || paused {
		t.Fatalf("wantPlaying %v paused %v: still playing after the renderer lost the stream", wantPlaying, paused)
	}
	for _, a := range r.Actions()[before:] {
		if a != "GetTransportInfo" && a != "GetPositionInfo" {
			t.Fatalf("sent %s to a renderer that came back empty", a)
		}
	}
	if resumeAt < 99 || resumeAt > 101 || index != 0 {
		t.Fatalf("resumeAt %v index %d, want the place where it was cut", resumeAt, index)
	}
	if Snapshot().Error == "" {
		t.Error("nothing said why it stopped")
	}
	// further polls change nothing
	Tick()
	if r.Count("Play") != 0 || r.Count("SetAVTransportURI") != 0 {
		t.Fatalf("a poll started something: %v", r.Actions())
	}
	// Play continues there
	if err := Resume(); err != nil {
		t.Fatal(err)
	}
	if st := r.Get(); st.State != "PLAYING" || at(st) < 99 || at(st) > 101 {
		t.Fatalf("after Play %+v at %v", st, at(st))
	}
}

// A renderer that missed a few polls and still plays the player's stream goes on.
func TestBackFromOfflineStillPlayingGoesOn(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "b"}
	playingAt(r, queue, 0, 100)
	Tick()
	r.Fail("GetTransportInfo", true)
	Tick()
	r.Fail("GetTransportInfo", false)
	r.With(func(s *testkit.RendererState) { s.Pos = 103 })
	Tick()
	if !wantPlaying || transport != "PLAYING" || Snapshot().Error != "" {
		t.Fatalf("wantPlaying %v transport %q error %q", wantPlaying, transport, Snapshot().Error)
	}
}

// Not playing, a renderer back from OFFLINE empty is nothing to act on.
func TestBackFromOfflineIdleIsQuiet(t *testing.T) {
	r := setup(t)
	r.Fail("GetTransportInfo", true)
	Tick()
	r.Fail("GetTransportInfo", false)
	r.With(func(s *testkit.RendererState) { s.State = "NO_MEDIA_PRESENT" })
	Tick()
	if Snapshot().Error != "" || transport != "NO_MEDIA_PRESENT" {
		t.Fatalf("error %q transport %q", Snapshot().Error, transport)
	}
}

// The Scan switch never drops the selected renderer, however long it is silent: it stays on
// the list and shows OFFLINE, as an SR11 does. Others still go. A device that answers from a
// new address is taken there.
func TestAutoScanKeepsTheSelected(t *testing.T) {
	resetState()
	t.Cleanup(resetState)
	var answer []upnp.Device
	old := rediscoverRenderers
	rediscoverRenderers = func(context.Context, time.Duration, []upnp.Device) ([]upnp.Device, error) { return answer, nil }
	t.Cleanup(func() { rediscoverRenderers = old })

	v := upnp.Device{UDN: "v", Name: "Valera", Location: "http://b:49494/d.xml"}
	f := upnp.Device{UDN: "f", Name: "FiiO", Location: "http://f/d.xml"}
	o := upnp.Device{UDN: "o", Name: "Other", Location: "http://o/d.xml"}
	renderers, selected = []upnp.Device{f, o, v}, "v"
	SetAutoScan(true)
	answer = []upnp.Device{f}
	AutoScanStep()
	mu.Lock()
	scanSeen["v"] = time.Now().Add(-time.Minute)
	scanSeen["o"] = time.Now().Add(-time.Minute)
	mu.Unlock()
	AutoScanStep()
	s := Snapshot()
	if len(s.Renderers) != 2 || s.Renderers[0].UDN != "f" || s.Renderers[1].UDN != "v" {
		t.Fatalf("renderers %+v, want FiiO and the silent selected Valera", s.Renderers)
	}
	// back on another port
	v2 := v
	v2.Location = "http://b:49500/d.xml"
	answer = []upnp.Device{f, v2}
	AutoScanStep()
	if d, ok := device(); !ok || d.Location != v2.Location {
		t.Fatalf("selected %+v, want it at its new address", d)
	}
}

// Refresh keeps the selected renderer on the list when it does not answer.
func TestRefreshKeepsTheSelected(t *testing.T) {
	r := setup(t)
	f := upnp.Device{UDN: "f", Name: "FiiO"}
	DiscoverRenderers = func(context.Context, time.Duration) ([]upnp.Device, error) { return []upnp.Device{f}, nil }
	Discover()
	s := Snapshot()
	if len(s.Renderers) != 2 || s.Selected != "r1" {
		t.Fatalf("renderers %+v selected %q", s.Renderers, s.Selected)
	}
	if d, ok := device(); !ok || d.UDN != r.Device.UDN {
		t.Fatalf("selected device %+v", d)
	}
}
