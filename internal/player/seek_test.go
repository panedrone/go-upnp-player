package player

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/stream"
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
// (live: 120, then 8, then 120 again). Paused: a seek over playing music is a Stop and a
// start there (jumpLocked).
func TestSeekSurvivesStalePositions(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 0, 8)
	pausedAt(r)
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
	pausedAt(r)
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

// A single odd position report: gmrender now and then answers GetPositionInfo with a
// position that is not where it plays (0:00:00, live on the BeagleBone). Taken at its
// word, it looked like a stream that strayed from the playlist, and the player seeked to
// the next track: a song cut short, and the stream reopened with a click on the coaxial
// output (2026-11-14). One poll must change nothing; a position that stays off does.
func TestOneOddPositionMovesNothing(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b", "c"}, 0, 0)
	mu.Lock()
	at, _ := streamTimeLocked(1, 50)
	mu.Unlock()
	r.With(func(s *testkit.RendererState) { s.Pos = at })
	index, position, streamPos = 1, 50, at
	Tick() // a poll as usual first: the player knows the stream and when it last asked
	odd := 1
	getPositionInfo = func(c context.Context, d upnp.Device) (upnp.PositionInfo, error) {
		pi, err := upnp.GetPositionInfo(c, d)
		if err == nil && odd > 0 {
			odd--
			pi.Position = 0
		}
		return pi, err
	}
	t.Cleanup(func() { getPositionInfo = upnp.GetPositionInfo })
	seeks := r.Count("Seek")
	for range 3 {
		Tick()
		if index != 1 {
			t.Fatalf("index %d after an odd position report, want 1", index)
		}
	}
	if r.Count("Seek") != seeks || r.Count("SetAVTransportURI") != 0 {
		t.Fatalf("seeks %d (were %d), setURI %d: one odd report must not move playback",
			r.Count("Seek"), seeks, r.Count("SetAVTransportURI"))
	}
}

// A start mid-album from STOPPED: the renderer holds nothing, so the stream begins at the
// chosen track and nothing is seeked. Opening the album's stream and seeking into it jumped
// audibly before the track played (the FiiO SR11, 2026-11-14). A jump to a later track of
// that stream is then a restart in place: no new stream, no Stop.
func TestStartMidAlbumBeginsAtTheTrack(t *testing.T) {
	r := setup(t)
	if err := PlayQueue([]string{"a", "b", "c"}, 1); err != nil {
		t.Fatal(err)
	}
	if runQ != 1 || index != 1 || at(r.Get()) != 0 || r.Count("Seek") != 0 {
		t.Fatalf("run from %d, index %d, at %v, seeks %d: want the run from b, no seek",
			runQ, index, at(r.Get()), r.Count("Seek"))
	}
	uri, setURI, stops := r.Get().URI, r.Count("SetAVTransportURI"), r.Count("Stop")
	seekAt = time.Time{} // the start has settled
	if err := PlayIndex(2); err != nil {
		t.Fatal(err)
	}
	if r.Get().URI != uri || r.Count("SetAVTransportURI") != setURI || r.Count("Stop") != stops {
		t.Fatalf("a jump within the stream started a new one: setURI %d->%d, stops %d->%d",
			setURI, r.Count("SetAVTransportURI"), stops, r.Count("Stop"))
	}
	if index != 2 || at(r.Get()) != 0 || silent(t, uri) {
		t.Fatalf("index %d at %v silent %v, want c from its start, music", index, at(r.Get()), silent(t, uri))
	}
}

// A jump within the stream while music plays: Pause, Seek, Play, each sent only after the
// renderer reported the state the previous one leads to (2026-11-14, the FiiO stuttered).
func TestJumpInStreamConfirmsEveryStep(t *testing.T) {
	r := setup(t)
	playsOn(t, r)
	playingAt(r, []string{"a", "b", "c"}, 0, 10)
	// the renderer reports itself PLAYING for a few polls after the Pause
	lag := 0
	var seekState string
	getTransportInfo = func(c context.Context, d upnp.Device) (upnp.TransportInfo, error) {
		ti, err := upnp.GetTransportInfo(c, d)
		if err == nil && ti.State == "PAUSED_PLAYBACK" && lag < 3 {
			lag++
			ti.State = "PLAYING"
		}
		return ti, err
	}
	seekRenderer = func(c context.Context, d upnp.Device, sec float64) error {
		ti, _ := getTransportInfo(c, d)
		seekState = ti.State
		return upnp.Seek(c, d, sec)
	}
	t.Cleanup(func() { getTransportInfo, seekRenderer = upnp.GetTransportInfo, upnp.Seek })
	if err := PlayIndex(2); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, a := range r.Actions() {
		if a == "Seek" || a == "Pause" || a == "Play" {
			order = append(order, a)
		}
	}
	// Stop: Pause, the stream let go and the renderer seeked where it is; then the start:
	// a seek to the track and Play
	if got := strings.Join(order, " "); !strings.HasSuffix(got, "Pause Seek Seek Play") || r.Get().State != "PLAYING" {
		t.Fatalf("order %v, state %s: want Pause, Seek, Seek, Play and playing", order, r.Get().State)
	}
	if lag < 3 || seekState != "PAUSED_PLAYBACK" {
		t.Fatalf("the seek went with the renderer reporting %q after %d lagging polls: it must wait for PAUSED", seekState, lag)
	}
	mu.Lock()
	target, _ := streamTimeLocked(2, 0)
	mu.Unlock()
	if got := r.Get().Pos; got < target || got > target+2 {
		t.Fatalf("at %.1f, want the track's start %.1f", got, target)
	}
}

// pausedAt pauses what playingAt set up, in the player and in the fake.
func pausedAt(r *testkit.Renderer) {
	paused = true
	r.With(func(s *testkit.RendererState) { s.State = "PAUSED_PLAYBACK" })
}

// The BeagleBone, paused, takes a seek and fetches the stream from the target, but goes on
// reporting its old position until Play. Its request from the target confirms the seek:
// waiting for the position alone ran out the polls on every jump, some 8 s (2026-11-14).
func TestPausedSeekIsConfirmedByTheRequestFromTheTarget(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b", "c"}, 0, 10)
	polls := 0
	frozen := -1.0
	getPositionInfo = func(c context.Context, d upnp.Device) (upnp.PositionInfo, error) {
		pi, err := upnp.GetPositionInfo(c, d)
		polls++
		if err == nil && r.Get().State == "PAUSED_PLAYBACK" {
			if frozen < 0 {
				frozen = pi.Position
			}
			pi.Position = frozen // paused: the old position, whatever the seeks did
		} else {
			frozen = -1
		}
		return pi, err
	}
	seekRenderer = func(c context.Context, d upnp.Device, sec float64) error {
		if err := upnp.Seek(c, d, sec); err != nil {
			return err
		}
		// the renderer fetches the stream from the target, as the BeagleBone does
		mu.Lock()
		id, _ := runIDOf(runURI)
		mu.Unlock()
		if src, ok := stream.ForRun(id, d.IP); ok {
			if rd, err := src.Open(44 + int64(sec*44100)*4); err == nil {
				_ = rd.Close()
			}
		}
		return nil
	}
	t.Cleanup(func() { getPositionInfo, seekRenderer = upnp.GetPositionInfo, upnp.Seek })
	if err := PlayIndex(2); err != nil {
		t.Fatal(err)
	}
	if polls > 25 {
		t.Fatalf("%d polls for one jump: the request from the target must confirm the seek", polls)
	}
	if index != 2 || r.Get().State != "PLAYING" {
		t.Fatalf("index %d state %s", index, r.Get().State)
	}
}
