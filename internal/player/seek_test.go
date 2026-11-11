package player

import (
	"context"
	"testing"
	"time"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/testkit"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

// staleAfterSeek replays what the BeagleBone does (2026-11-11): every seek is taken at
// once, but GetPositionInfo keeps reporting the position from before it for the next
// polls polls. The seams pass everything else on to the fake renderer.
func staleAfterSeek(t *testing.T, polls int) {
	t.Helper()
	left, old := 0, 0.0
	seekRenderer = func(c context.Context, d upnp.Device, sec float64) error {
		pi, err := upnp.GetPositionInfo(c, d)
		if err != nil {
			return err
		}
		if err := upnp.Seek(c, d, sec); err != nil {
			return err
		}
		left, old = polls, pi.Position
		return nil
	}
	getPositionInfo = func(c context.Context, d upnp.Device) (upnp.PositionInfo, error) {
		pi, err := upnp.GetPositionInfo(c, d)
		if err == nil && left > 0 {
			left--
			pi.Position = old
		}
		return pi, err
	}
	t.Cleanup(func() {
		seekRenderer, getPositionInfo = upnp.Seek, upnp.GetPositionInfo
	})
}

// A click on a later track of the run is a seek in its stream (ADR-016). The polls
// that still see the old position must not take it for a stream that strayed from
// the playlist: that started the track after the clicked one (live, 2026-11-11:
// a click on "Visions" played "Hope").
func TestJumpInRunSurvivesStalePositions(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b", "c"}, 0, 100)
	staleAfterSeek(t, 2)
	if err := PlayIndex(2); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		Tick()
		if index != 2 || position > 1 {
			t.Fatalf("index %d position %v, want track 2 at its start", index, position)
		}
	}
	if r.Count("SetAVTransportURI") != 0 || r.Count("SetNextAVTransportURI") != 0 || !wantPlaying || lastErr != "" {
		t.Fatalf("setURI %d setNext %d want %v err %q: the jump must stay a seek",
			r.Count("SetAVTransportURI"), r.Count("SetNextAVTransportURI"), wantPlaying, lastErr)
	}
}

// A click on an earlier track of the run: the old position used to turn the index
// back to the track that was playing.
func TestJumpBackInRunSurvivesStalePositions(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b", "c"}, 0, 0)
	mu.Lock()
	at, _ := streamTimeLocked(1, 50)
	mu.Unlock()
	r.With(func(s *testkit.RendererState) { s.Pos = at })
	index, position, streamPos = 1, 50, at
	staleAfterSeek(t, 2)
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		Tick()
		if index != 0 || position > 1 {
			t.Fatalf("index %d position %v, want track 0 at its start", index, position)
		}
	}
}

// The seek slider: the old position must not pull the knob back for a poll
// (live: 120, then 8, then 120 again).
func TestSeekSurvivesStalePositions(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 0, 8)
	staleAfterSeek(t, 2)
	if err := Seek(120); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		Tick()
		if position != 120 || index != 0 {
			t.Fatalf("index %d position %v, want 120 in track 0", index, position)
		}
	}
	if r.Count("SetAVTransportURI") != 0 {
		t.Fatal("a seek within the track must not restart it")
	}
}

// A seek the renderer dropped: after services.SeekLandWithin its position is the truth.
func TestDroppedSeekIsTakenAtTheRenderersWord(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 0, 8)
	staleAfterSeek(t, 1000)
	if err := Seek(120); err != nil {
		t.Fatal(err)
	}
	Tick()
	if position != 120 {
		t.Fatalf("position %v right after the seek, want 120", position)
	}
	mu.Lock()
	seekAt = seekAt.Add(-time.Minute)
	mu.Unlock()
	Tick()
	if position != 8 || index != 0 {
		t.Fatalf("index %d position %v, want the renderer's 8 in track 0", index, position)
	}
}

// Real tracks do not last whole seconds, so the tracks of a run start at fractions of a
// second in its stream, while seek targets and RelTime are whole seconds, cut down: Next
// lands just before the next track. Read as the end of the playing track, that made
// the poll start the track after the next one (live, 2026-11-11: Next skipped a track).
func TestNextLandingJustBeforeTheTrackDoesNotSkipIt(t *testing.T) {
	r := setup(t)
	odd := map[string]*library.Track{}
	for id, sec := range map[string]float64{"x": 240.4, "y": 180.7, "z": 200.2} {
		odd[id] = &library.Track{ID: id, Title: id, Duration: sec, SampleRate: 44100, Channels: 2, Bits: 16, End: uint64(sec * 44100)}
	}
	lookupTrack = func(id string) *library.Track { return odd[id] }
	playingAt(r, []string{"x", "y", "z"}, 0, 100)
	for want := 1; want <= 2; want++ {
		if err := Next(); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			Tick()
			if index != want || position > 1 {
				t.Fatalf("index %d position %v, want track %d at its start", index, position, want)
			}
		}
	}
	if r.Count("SetAVTransportURI") != 0 || r.Count("SetNextAVTransportURI") != 0 || lastErr != "" {
		t.Fatalf("setURI %d setNext %d err %q: Next within a run must stay a seek",
			r.Count("SetAVTransportURI"), r.Count("SetNextAVTransportURI"), lastErr)
	}
}

// A stream that starts into its first track (a resume after an update, ADR-015) moves
// on into the next track of the run like any other.
func TestStreamFromAPositionMovesOnIntoTheNextTrack(t *testing.T) {
	r := setup(t)
	odd := map[string]*library.Track{}
	for id, sec := range map[string]float64{"x": 198.27, "y": 385.52} {
		odd[id] = &library.Track{ID: id, Title: id, Duration: sec, SampleRate: 44100, Channels: 2, Bits: 16, End: uint64(sec * 44100)}
	}
	lookupTrack = func(id string) *library.Track { return odd[id] }
	queue = []string{"x", "y"}
	cmdMu.Lock()
	err := startLocked(0, 184)
	cmdMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	lastCmd = time.Now().Add(-time.Minute)
	into := runStarts[1]
	mu.Unlock()
	for _, p := range []float64{10, into - 0.5, into + 1, into + 3} {
		r.With(func(s *testkit.RendererState) { s.Pos = p })
		Tick()
	}
	if index != 1 || position < 2 || position > 4 || r.Count("SetAVTransportURI") != 1 || lastErr != "" {
		t.Fatalf("index %d position %v setURI %d err %q: want track 1 a few seconds in, no new start",
			index, position, r.Count("SetAVTransportURI"), lastErr)
	}
}
