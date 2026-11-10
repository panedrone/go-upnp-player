package player

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
	"github.com/panedrone/go-upnp-player/internal/testkit"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

func TestMain(m *testing.M) {
	cleanup := testkit.IsolateUserDirs()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

var tracks = map[string]*library.Track{
	"a": {ID: "a", Title: "A", Duration: 240, SampleRate: 44100, Channels: 2, Bits: 16, End: 240 * 44100, FileSize: 1000},
	"b": {ID: "b", Title: "B", Duration: 180, SampleRate: 44100, Channels: 2, Bits: 16, End: 180 * 44100, FromCue: true},
	"c": {ID: "c", Title: "C", Duration: 200, SampleRate: 44100, Channels: 2, Bits: 16, End: 200 * 44100, FileSize: 2000},
}

// setup resets the package state and selects one fake renderer.
func setup(t *testing.T) *testkit.Renderer {
	t.Helper()
	resetState()
	lookupTrack = func(id string) *library.Track { return tracks[id] }
	sleep = func(time.Duration) {}
	t.Cleanup(func() {
		lookupTrack, sleep, DiscoverRenderers = library.Get, time.Sleep, upnp.Discover
		stream.SetEQ(services.EQSettings{})
		resetState()
	})
	r := newRenderer(t, "r1")
	renderers = []upnp.Device{r.Device}
	selected = "r1"
	mediaPort = 8090
	return r
}

func newRenderer(t *testing.T, udn string) *testkit.Renderer {
	r := testkit.NewRenderer(t, udn)
	r.SetLength("a", 240)
	r.SetLength("b", 180)
	r.SetLength("c", 200)
	r.SetLength("old", 300)
	return r
}

func resetState() {
	renderers, selected, queue, queueVersion, source = nil, "", nil, 0, ""
	index, wantPlaying, paused, lastCmd, nextURL = -1, false, false, time.Time{}, ""
	transport, position, duration, volume, lastErr = "", 0, 0, -1, ""
	discovering, mediaPort, tickCount, resumeAt = false, 0, 0, -1
	frozenSince, stalled = time.Time{}, false
	lastPollAt, lastTrackURI, racingTicks, racingIndex, racingFrom, racingFixAt = time.Time{}, "", 0, 0, 0, time.Time{}
	_ = config.Update(func(c *config.Config) { *c = config.Config{} })
}

// playingAt makes the player and the renderer agree that queue[i] is playing.
func playingAt(r *testkit.Renderer, ids []string, i int, pos float64) {
	queue, index, wantPlaying, position = ids, i, true, pos
	lastCmd = time.Now().Add(-time.Minute)
	r.With(func(s *testkit.RendererState) {
		s.State, s.URI, s.Pos = "PLAYING", "http://x/"+ids[i], pos
	})
}

func TestStartFromStoppedSetsURIAndQueuesNext(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "b"}
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	st := r.Get()
	if st.State != "PLAYING" || !strings.Contains(st.URI, "127.0.0.1:8090/media/track/a.flac?ct=") {
		t.Fatalf("state %s, uri %s", st.State, st.URI)
	}
	if !strings.Contains(st.Next, "/media/track/b.wav") {
		t.Fatalf("next %q: a cue track goes as WAV", st.Next)
	}
	if index != 0 || !wantPlaying || paused || duration != 240 {
		t.Fatalf("index %d want %v paused %v duration %v", index, wantPlaying, paused, duration)
	}
	if got := config.Get().Playlist; got.Index != 0 || len(got.IDs) != 2 {
		t.Fatalf("saved playlist %+v", got)
	}
}

func TestBusyRendererSwitchesThroughNextURI(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"old", "a"}, 0, 20)
	if err := PlayIndex(1); err != nil {
		t.Fatal(err)
	}
	if n := r.Count("SetAVTransportURI"); n != 0 {
		t.Fatalf("SetAVTransportURI sent %d times to a playing renderer", n)
	}
	if got := r.Last("Seek")["Target"]; got != services.FormatSeekTarget(299) {
		t.Fatalf("seek target %s", got)
	}
	if st := r.Get(); !strings.Contains(st.URI, "/a.flac") || st.Pos != 0 {
		t.Fatalf("renderer on %s at %v", st.URI, st.Pos)
	}
	if r.Count("Stop") != 0 || r.Count("Play") != 0 {
		t.Fatalf("stop %d play %d", r.Count("Stop"), r.Count("Play"))
	}
}

func TestPausedRendererResumesAfterSwitch(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"old", "a"}, 0, 20)
	r.With(func(s *testkit.RendererState) { s.State = "PAUSED_PLAYBACK" })
	if err := PlayIndex(1); err != nil {
		t.Fatal(err)
	}
	if r.Count("Play") != 1 || r.Get().State != "PLAYING" {
		t.Fatalf("play %d state %s", r.Count("Play"), r.Get().State)
	}
}

func TestSameTrackRestartsInPlace(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	uri, _ := media(r.Device, tracks["a"])
	r.With(func(s *testkit.RendererState) { s.State, s.URI, s.Pos = "PAUSED_PLAYBACK", uri, 50 })
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	if r.Get().Pos != 0 || r.Count("SetNextAVTransportURI") != 0 || r.Count("Play") != 1 {
		t.Fatalf("pos %v setnext %d play %d", r.Get().Pos, r.Count("SetNextAVTransportURI"), r.Count("Play"))
	}
}

func TestSwitchFailureIsReported(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"old", "a"}, 0, 20)
	r.With(func(s *testkit.RendererState) { s.NoAdvance = true })
	err := PlayIndex(1)
	if err == nil || !strings.Contains(err.Error(), "did not switch") {
		t.Fatalf("got %v", err)
	}
	if n := r.Count("SetNextAVTransportURI"); n != 3 {
		t.Fatalf("%d attempts, want 3", n)
	}
	if index != 0 || lastErr == "" {
		t.Fatalf("index %d lastErr %q: a failed switch must not look like a start", index, lastErr)
	}
}

func TestSwitchFallsBackWhenRendererStops(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"old", "a"}, 0, 20)
	r.With(func(s *testkit.RendererState) { s.NoAdvance = true })
	sleep = func(time.Duration) { r.With(func(s *testkit.RendererState) { s.State = "STOPPED" }) }
	if err := PlayIndex(1); err != nil {
		t.Fatal(err)
	}
	if st := r.Get(); st.State != "PLAYING" || !strings.Contains(st.URI, "/a.flac") || index != 1 {
		t.Fatalf("%+v index %d", st, index)
	}
}

func TestTickDetectsStall(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a"}, 0, 25)
	Tick()
	if stalled {
		t.Fatal("stalled on the first poll")
	}
	frozenSince = time.Now().Add(-services.StallAfter)
	Tick()
	if !Snapshot().Stalled {
		t.Fatal("a frozen position must be reported")
	}
	r.With(func(s *testkit.RendererState) { s.Pos = 26 })
	Tick()
	if stalled {
		t.Fatal("the position moved: no longer stalled")
	}
}

// race moves the fake renderer's position ahead by sec and polls it a second after
// the previous poll (the polls themselves run back to back, too fast for the clock).
func race(r *testkit.Renderer, sec float64) {
	r.With(func(s *testkit.RendererState) { s.Pos += sec })
	mu.Lock()
	lastPollAt = time.Now().Add(-time.Second)
	mu.Unlock()
	Tick()
}

func TestTickRestartsRacingRenderer(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"old", "a", "b"}, 1, 25)
	r.With(func(s *testkit.RendererState) { s.StopAllowed = true })
	Tick() // baseline
	race(r, 1)
	race(r, 5) // ~5x, as seen live
	race(r, 5)
	if r.Count("Stop") != 0 {
		t.Fatal("two jumps may be late seeks: no restart yet")
	}
	race(r, 5)
	if r.Count("Stop") != 1 {
		t.Fatalf("Stop sent %d times, want 1", r.Count("Stop"))
	}
	st := r.Get()
	if st.State != "PLAYING" || !strings.Contains(st.URI, "/a.flac") || st.Pos != 26 || index != 1 {
		t.Fatalf("restarted as %+v, index %d; want a at 26 s", st, index)
	}
	if Snapshot().Error != "" {
		t.Fatalf("a successful restart is not an error: %q", Snapshot().Error)
	}
}

func TestTickRacingAgainSoonPausesAndReports(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a"}, 0, 25)
	r.With(func(s *testkit.RendererState) { s.StopAllowed = true })
	racingFixAt = time.Now()
	Tick()
	race(r, 38)
	race(r, 38)
	race(r, 38)
	if r.Count("Stop") != 0 || r.Get().State != "PAUSED_PLAYBACK" || !paused {
		t.Fatalf("Stop %d, state %s, paused %v", r.Count("Stop"), r.Get().State, paused)
	}
	if !strings.Contains(Snapshot().Error, "restart the renderer") {
		t.Fatalf("error %q", Snapshot().Error)
	}
}

func TestTickNormalPlaybackAndSeeksAreNotRacing(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 0, 25)
	Tick()
	for range 5 {
		race(r, 1)
		race(r, 2) // RelTime rounding
	}
	race(r, 100) // a seek forward
	race(r, 1)
	race(r, 100) // two more, not three in a row
	race(r, 100)
	race(r, 1)
	race(r, -150)
	if r.Count("Stop") != 0 || racingTicks != 0 {
		t.Fatalf("Stop %d, racing run %d", r.Count("Stop"), racingTicks)
	}
	wantPlaying, paused = true, true
	r.With(func(s *testkit.RendererState) { s.State = "PAUSED_PLAYBACK" })
	race(r, 50)
	race(r, 50)
	if r.Count("Stop") != 0 {
		t.Fatal("a paused renderer is left alone")
	}
}

func TestStallIsClearedByPauseAndOffline(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a"}, 0, 25)
	Tick()
	frozenSince = time.Now().Add(-services.StallAfter)
	r.With(func(s *testkit.RendererState) { s.State = "PAUSED_PLAYBACK" })
	Tick()
	if stalled {
		t.Fatal("paused is not stalled")
	}
	stalled = true
	r.Close()
	Tick()
	if stalled {
		t.Fatal("offline is reported as offline")
	}
}

func TestResumeOnStalledRendererDoesARealStart(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"old", "a"}, 1, 25)
	r.With(func(s *testkit.RendererState) { s.URI, s.NoAdvance = "http://x/old", true })
	stalled = true
	err := Resume()
	if err == nil || r.Count("Play") != 0 {
		t.Fatalf("err %v, plain Play sent %d times", err, r.Count("Play"))
	}
}

func TestSeekIsRepeatedUntilItLands(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	r.With(func(s *testkit.RendererState) { s.IgnoreSeeks = 2 })
	cmdMu.Lock()
	err := startLocked(0, 100)
	cmdMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if n := r.Count("Seek"); n != 3 {
		t.Fatalf("%d seeks, want 3", n)
	}
	if r.Get().Pos != 100 {
		t.Fatalf("landed at %v", r.Get().Pos)
	}
}

func TestStartErrors(t *testing.T) {
	r := setup(t)
	queue = []string{"gone", "a"}
	if err := PlayIndex(5); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("got %v", err)
	}
	if err := PlayIndex(0); err == nil || !strings.Contains(err.Error(), "not in the library") {
		t.Fatalf("got %v", err)
	}
	if lastErr == "" {
		t.Fatal("the error must reach the UI")
	}
	r.Fail("SetAVTransportURI", true)
	if err := PlayIndex(1); err == nil {
		t.Fatal("want SetURI error")
	}
	r.Fail("SetAVTransportURI", false)
	r.Fail("Play", true)
	if err := PlayIndex(1); err == nil {
		t.Fatal("want Play error")
	}
	r.Fail("Play", false)
	selected = "nobody"
	if err := PlayIndex(1); err == nil || !strings.Contains(err.Error(), "no renderer") {
		t.Fatalf("got %v", err)
	}
}

func TestPlayQueue(t *testing.T) {
	r := setup(t)
	if err := PlayQueue([]string{"a"}, 3); err == nil {
		t.Fatal("want index error")
	}
	if err := PlayQueue([]string{"a", "b"}, 1); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Get().URI, "/b.wav") || queueVersion != 1 {
		t.Fatalf("uri %s version %d", r.Get().URI, queueVersion)
	}
}

func TestSetQueueKeepsPlayingTrack(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 1, 10)
	SetQueue([]string{"c", "b", "a"}, "")
	if index != 1 {
		t.Fatalf("index %d, want the playing track found in the new list", index)
	}
	if !strings.Contains(r.Get().Next, "/a.flac") {
		t.Fatalf("next %s", r.Get().Next)
	}
	SetQueue([]string{"c"}, "")
	if index != -1 {
		t.Fatalf("index %d after the playing track left the list", index)
	}
}

func TestSetQueueIdleDoesNotTouchRenderer(t *testing.T) {
	r := setup(t)
	SetQueue([]string{"a", "b"}, "")
	if r.Count("SetNextAVTransportURI") != 0 || queueVersion != 1 {
		t.Fatalf("setnext %d version %d", r.Count("SetNextAVTransportURI"), queueVersion)
	}
}

// The tree highlights the node a playlist was built from; it survives a restart,
// and a playlist started from a list of ids (no tree node) clears it.
func TestPlaylistSourceIsKept(t *testing.T) {
	setup(t)
	source = "/music/old"
	if err := PlayQueue([]string{"a"}, 0); err != nil {
		t.Fatal(err)
	}
	if source != "" {
		t.Fatalf("source %q after a playlist without a tree node", source)
	}
	SetQueue([]string{"a", "b"}, "/music/album.cue")
	if s := Snapshot(); s.Source != "/music/album.cue" {
		t.Fatalf("snapshot source %q", s.Source)
	}
	if got := config.Get().Playlist.Source; got != "/music/album.cue" {
		t.Fatalf("saved source %q", got)
	}
	source = ""
	Init(0) // a restart: last, it rereads the renderer and the port too
	if source != "/music/album.cue" {
		t.Fatalf("source after restart %q", source)
	}
}

func TestSelectWhilePlayingMovesTrackWithPosition(t *testing.T) {
	r1 := setup(t)
	r2 := newRenderer(t, "r2")
	renderers = append(renderers, r2.Device)
	playingAt(r1, []string{"a"}, 0, 50)
	if err := Select("r2"); err != nil {
		t.Fatal(err)
	}
	if r1.Count("Pause") != 1 || r1.Count("Stop") != 0 {
		t.Fatalf("old renderer: pause %d stop %d", r1.Count("Pause"), r1.Count("Stop"))
	}
	if st := r2.Get(); st.State != "PLAYING" || st.Pos != 50 {
		t.Fatalf("new renderer %+v", st)
	}
	if config.Get().Renderer != "r2" {
		t.Fatalf("saved renderer %q", config.Get().Renderer)
	}
}

func TestSelectWhilePausedResumesThereOnPlay(t *testing.T) {
	r1 := setup(t)
	r2 := newRenderer(t, "r2")
	renderers = append(renderers, r2.Device)
	playingAt(r1, []string{"a"}, 0, 50)
	paused = true
	if err := Select("r2"); err != nil {
		t.Fatal(err)
	}
	if r2.Count("Play") != 0 || resumeAt != 50 {
		t.Fatalf("play %d resumeAt %v", r2.Count("Play"), resumeAt)
	}
	if err := Resume(); err != nil {
		t.Fatal(err)
	}
	if st := r2.Get(); st.State != "PLAYING" || st.Pos != 50 || resumeAt != -1 {
		t.Fatalf("%+v resumeAt %v", st, resumeAt)
	}
}

func TestResume(t *testing.T) {
	r := setup(t)
	if err := Resume(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("got %v", err)
	}
	queue = []string{"a"}
	if err := Resume(); err != nil { // built but never played: from the first track
		t.Fatal(err)
	}
	if index != 0 || r.Count("SetAVTransportURI") != 1 {
		t.Fatalf("index %d", index)
	}
	paused = true
	if err := Resume(); err != nil {
		t.Fatal(err)
	}
	if paused || r.Count("SetAVTransportURI") != 1 {
		t.Fatalf("paused %v, setURI %d", paused, r.Count("SetAVTransportURI"))
	}
}

func TestPauseAndStopNeverSendStop(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a"}, 0, 5)
	if err := Pause(); err != nil || !paused {
		t.Fatalf("err %v paused %v", err, paused)
	}
	resumeAt = 7
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if wantPlaying || paused || resumeAt != -1 || r.Count("Pause") != 2 || r.Count("Stop") != 0 {
		t.Fatalf("want %v paused %v resume %v pause %d", wantPlaying, paused, resumeAt, r.Count("Pause"))
	}
}

func TestNextAndPrev(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 1, 10)
	if err := Next(); err != nil || r.Count("Seek") != 0 {
		t.Fatalf("end of queue: err %v seeks %d", err, r.Count("Seek"))
	}
	if err := Prev(); err != nil { // deep into the track: back to its start
		t.Fatal(err)
	}
	if r.Get().Pos != 0 || index != 1 {
		t.Fatalf("pos %v index %d", r.Get().Pos, index)
	}
	position = 1
	r.SetLength("b", 180)
	if err := Prev(); err != nil {
		t.Fatal(err)
	}
	if index != 0 {
		t.Fatalf("index %d", index)
	}
	if err := Next(); err != nil || index != 1 {
		t.Fatalf("err %v index %d", err, index)
	}
}

func TestSeekAndVolume(t *testing.T) {
	r := setup(t)
	if err := Seek(33); err != nil || position != 33 || r.Get().Pos != 33 {
		t.Fatalf("err %v pos %v", err, position)
	}
	if err := SetVolume(150); err != nil || volume != 100 || r.Get().Volume != 100 {
		t.Fatalf("err %v volume %d", err, volume)
	}
	r.Fail("Seek", true)
	if err := Seek(1); err == nil || position != 33 {
		t.Fatalf("err %v pos %v", err, position)
	}
	r.Fail("SetVolume", true)
	if err := SetVolume(1); err == nil || volume != 100 {
		t.Fatalf("err %v volume %d", err, volume)
	}
	r.Fail("Pause", true)
	if err := Pause(); err == nil || paused {
		t.Fatalf("err %v paused %v", err, paused)
	}
	r.Fail("Play", true)
	wantPlaying, paused = true, true
	queue, index = []string{"a"}, 0
	if err := Resume(); err == nil || !paused {
		t.Fatalf("err %v paused %v", err, paused)
	}
}

func TestDoWithoutRenderer(t *testing.T) {
	setup(t)
	selected = ""
	if err := Pause(); err == nil {
		t.Fatal("want an error without a renderer")
	}
}

func TestTickAdvancesWhenRendererMovedOn(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b", "c"}, 0, 239)
	nextURL = "http://x/b"
	r.With(func(s *testkit.RendererState) { s.URI, s.Pos = nextURL, 1 })
	Tick()
	if index != 1 || config.Get().Playlist.Index != 1 {
		t.Fatalf("index %d saved %d", index, config.Get().Playlist.Index)
	}
	if !strings.Contains(r.Get().Next, "/c.flac") {
		t.Fatalf("next %s", r.Get().Next)
	}
	if transport != "PLAYING" || duration != 180 || position != 1 {
		t.Fatalf("transport %s duration %v position %v", transport, duration, position)
	}
}

func TestTickIgnoresEarlyURIChange(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 0, 100)
	nextURL = "http://x/b"
	// gmrender names next as current minutes early but still reports the old length
	r.SetLength("b", 240)
	r.With(func(s *testkit.RendererState) { s.URI = nextURL })
	Tick()
	if index != 0 {
		t.Fatalf("index %d: advanced on the early URI report", index)
	}
}

func TestTickStartsNextWhenStopped(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 0, 10)
	r.With(func(s *testkit.RendererState) { s.State, s.URI = "STOPPED", "" })
	Tick()
	if index != 1 || !strings.Contains(r.Get().URI, "/b.wav") {
		t.Fatalf("index %d uri %s", index, r.Get().URI)
	}
}

func TestTickStartNextFailureIsNotRetriedEveryPoll(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 0, 10)
	r.With(func(s *testkit.RendererState) { s.State, s.URI = "STOPPED", "" })
	r.Fail("Play", true)
	Tick()
	Tick()
	Tick()
	if n := r.Count("Play"); n != 1 {
		t.Fatalf("Play sent %d times, want 1", n)
	}
	if wantPlaying || Snapshot().Error == "" {
		t.Fatalf("wantPlaying %v, error %q", wantPlaying, Snapshot().Error)
	}
}

func TestTickFinishesQueue(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a"}, 0, 10)
	r.With(func(s *testkit.RendererState) { s.State = "STOPPED" })
	Tick()
	if wantPlaying {
		t.Fatal("still wants to play after the end of the queue")
	}
}

func TestTickOffline(t *testing.T) {
	r := setup(t)
	r.Close()
	Tick()
	if transport != "OFFLINE" {
		t.Fatalf("transport %q", transport)
	}
}

func TestTickPollsVolumeEveryTenth(t *testing.T) {
	r := setup(t)
	r.With(func(s *testkit.RendererState) { s.Volume = 33 })
	tickCount = 8
	Tick()
	if volume != -1 {
		t.Fatalf("volume %d polled too early", volume)
	}
	Tick()
	if volume != 33 {
		t.Fatalf("volume %d", volume)
	}
}

func TestTickSkipsDuringCommand(t *testing.T) {
	r := setup(t)
	cmdMu.Lock()
	Tick()
	cmdMu.Unlock()
	if r.Count("GetTransportInfo") != 0 {
		t.Fatal("polled in the middle of a command")
	}
	selected = ""
	Tick() // no renderer: nothing to poll
	if r.Count("GetTransportInfo") != 0 {
		t.Fatal("polled without a renderer")
	}
}

func eqOn() services.EQSettings {
	g := make([]float64, len(services.EQBands))
	g[0] = 3
	return services.EQSettings{Enabled: true, Gains: g}
}

func TestSetEQRestartsPlainFLACOnTypeChange(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	if err := SetEQ(eqOn()); err != nil {
		t.Fatal(err)
	}
	if !config.Get().EQ.Active() || !stream.EQActive() {
		t.Fatal("settings not applied")
	}
	if n := r.Count("Seek"); n != 1 { // same-URI check fails (flac -> wav): gapless switch or restart
		t.Fatalf("%d seeks", n)
	}
	if got := r.Last("SetNextAVTransportURI")["NextURI"]; !strings.Contains(got, "/a.wav") {
		t.Fatalf("switched to %s", got)
	}
}

func TestSetEQReseeksCueTrack(t *testing.T) {
	r := setup(t)
	queue = []string{"b"}
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	position = 30
	if err := SetEQ(eqOn()); err != nil {
		t.Fatal(err)
	}
	if r.Get().Pos != 30 || r.Count("SetNextAVTransportURI") != 0 {
		t.Fatalf("pos %v", r.Get().Pos)
	}
}

func TestSetEQLeavesRawFLACAndIdleAlone(t *testing.T) {
	r := setup(t)
	if err := SetEQ(services.EQSettings{}); err != nil { // idle
		t.Fatal(err)
	}
	queue = []string{"a"}
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	seeks := r.Count("Seek")
	if err := SetEQ(services.EQSettings{Gains: eqOn().Gains}); err != nil { // disabled: still raw
		t.Fatal(err)
	}
	if r.Count("Seek") != seeks {
		t.Fatal("raw FLAC re-requested for an EQ that does not apply")
	}
}

func TestDiscover(t *testing.T) {
	r := setup(t)
	selected = ""
	DiscoverRenderers = func(context.Context, time.Duration) ([]upnp.Device, error) {
		return []upnp.Device{r.Device}, nil
	}
	Discover()
	if selected != "r1" || len(renderers) != 1 || discovering || volume != 40 {
		t.Fatalf("selected %q renderers %d discovering %v volume %d", selected, len(renderers), discovering, volume)
	}
	selected = "other" // a saved choice is not replaced by whatever was found
	Discover()
	if selected != "other" {
		t.Fatalf("selected %q", selected)
	}
	DiscoverRenderers = func(context.Context, time.Duration) ([]upnp.Device, error) {
		return nil, errors.New("no network")
	}
	Discover()
	if lastErr != "no network" || len(renderers) != 1 {
		t.Fatalf("lastErr %q renderers %d", lastErr, len(renderers))
	}
	discovering = true
	calls := 0
	DiscoverRenderers = func(context.Context, time.Duration) ([]upnp.Device, error) {
		calls++
		return nil, nil
	}
	Discover()
	if calls != 0 {
		t.Fatal("two discoveries at once")
	}
}

func TestSnapshotQueue(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "gone", "b"}, 0, 5)
	Build = "x1"
	Version = "1.2"
	t.Cleanup(func() { Build, Version = "", "" })
	s := Snapshot()
	if s.Track == nil || s.Track.ID != "a" || s.QueueLen != 3 || s.Build != "x1" || s.Version != "1.2" {
		t.Fatalf("%+v", s)
	}
	_, list := Queue()
	if len(list) != 2 {
		t.Fatalf("%d tracks: missing ones must be skipped", len(list))
	}
}

func TestInitRestoresPlaylist(t *testing.T) {
	setup(t)
	if err := config.Update(func(c *config.Config) {
		c.Renderer = "saved"
		c.Playlist = config.Playlist{IDs: []string{"a", "b"}, Index: 1}
	}); err != nil {
		t.Fatal(err)
	}
	Init(9000)
	if mediaPort != 9000 || selected != "saved" || index != 1 || len(queue) != 2 || wantPlaying {
		t.Fatalf("port %d selected %q index %d queue %v want %v", mediaPort, selected, index, queue, wantPlaying)
	}
	_ = config.Update(func(c *config.Config) { c.Playlist.Index = 9 })
	Init(9000)
	if index != -1 {
		t.Fatalf("index %d from a broken config", index)
	}
}

func TestShutdownPausesOnlyWhenPlaying(t *testing.T) {
	r := setup(t)
	Shutdown()
	if r.Count("Pause") != 0 {
		t.Fatal("paused an idle renderer")
	}
	playingAt(r, []string{"a"}, 0, 5)
	Shutdown()
	if r.Count("Pause") != 1 || r.Count("Stop") != 0 {
		t.Fatalf("pause %d stop %d", r.Count("Pause"), r.Count("Stop"))
	}
}

func TestMediaPicksContainerAndMime(t *testing.T) {
	r := setup(t)
	uri, meta := media(r.Device, tracks["a"])
	if !strings.Contains(uri, "a.flac?ct=audio%2Fflac") || !strings.Contains(meta, "/media/art/a") {
		t.Fatalf("%s\n%s", uri, meta)
	}
	stream.SetEQ(eqOn())
	if uri, _ = media(r.Device, tracks["a"]); !strings.Contains(uri, "a.wav?ct=audio%2Fx-wav") {
		t.Fatal(uri)
	}
}

// An update or a restart while paused (or playing) is not a Stop: Play continues the
// track at the same second, with no detour through its opening.
func TestResumeAfterRestartContinuesAtPosition(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 1, 42)
	uri, _ := media(r.Device, tracks["b"])
	r.With(func(s *testkit.RendererState) { s.State, s.URI = "PAUSED_PLAYBACK", uri })
	paused = true
	Shutdown()
	if got := config.Get().Playlist.Position; got != 42 {
		t.Fatalf("saved position %v", got)
	}

	// a new process: nothing in memory but the config
	_ = config.Update(func(c *config.Config) { c.Renderer = "r1" })
	queue, index, wantPlaying, paused, resumeAt = nil, -1, false, false, -1
	Init(8090)
	if index != 1 || resumeAt != 42 {
		t.Fatalf("index %d resumeAt %v", index, resumeAt)
	}
	if err := Resume(); err != nil {
		t.Fatal(err)
	}
	st := r.Get()
	if st.State != "PLAYING" || st.Pos != 42 || r.Count("Seek") != 1 {
		t.Fatalf("%+v seeks %d", st, r.Count("Seek"))
	}
	if got := config.Get().Playlist.Position; got != 0 || resumeAt != -1 {
		t.Fatalf("after resume: saved %v resumeAt %v", got, resumeAt)
	}
}

// A user Stop still means "start over", and so does quitting while stopped.
func TestShutdownWhenStoppedSavesNoPosition(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a"}, 0, 42)
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	Shutdown()
	if got := config.Get().Playlist.Position; got != 0 {
		t.Fatalf("saved position %v", got)
	}
}
