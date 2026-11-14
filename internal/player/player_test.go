package player

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	// another format: a run (ADR-016) ends before it
	"hi": {ID: "hi", Title: "HI", Duration: 200, SampleRate: 96000, Channels: 2, Bits: 24, End: 200 * 96000},
}

// setup resets the package state and selects one fake renderer.
func setup(t *testing.T) *testkit.Renderer {
	t.Helper()
	resetState()
	keepOutputOpen = true // the default; stopSafe below turns it off
	lookupTrack = func(id string) *library.Track { return tracks[id] }
	sleep = func(time.Duration) {}
	t.Cleanup(func() {
		halting.Wait() // old renderers halted by Select
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

// at is where a renderer really is in the track: its position in the player's current
// stream turned into the track's (a run of tracks, ADR-016, that may start into its
// first track, ADR-015).
func at(st testkit.RendererState) float64 {
	mu.Lock()
	defer mu.Unlock()
	if st.URI != "" && st.URI == runURI {
		_, p := locateLocked(st.Pos, -1)
		return p
	}
	return st.Pos
}

func newRenderer(t *testing.T, udn string) *testkit.Renderer {
	r := testkit.NewRenderer(t, udn)
	r.SetLength("a", 240)
	r.SetLength("b", 180)
	r.SetLength("c", 200)
	r.SetLength("old", 300)
	r.SetLength("hi", 200)
	return r
}

func resetState() {
	renderers, selected, queue, queueVersion, source = nil, "", nil, 0, ""
	index, wantPlaying, paused, lastCmd, nextURL = -1, false, false, time.Time{}, ""
	transport, position, duration, volume, lastErr = "", 0, 0, -1, ""
	discovering, mediaPort, tickCount, resumeAt = false, 0, 0, -1
	frozenSince, stalled = time.Time{}, false
	lastPollAt, lastTrackURI, racingTicks, racingIndex, racingFrom, racingFixAt = time.Time{}, "", 0, 0, 0, time.Time{}
	pendingIndex, pendingPos, pendingSince, pendingTry, pendingTries = 0, 0, time.Time{}, time.Time{}, 0
	offset, finishedAt = 0, time.Time{}
	runQ, runIDs, runStarts, runDurs, runLen, runURI, nextRun, streamPos = 0, nil, nil, nil, 0, "", runPlan{}, 0
	seekTo, seekAt = 0, time.Time{}
	_ = config.Update(func(c *config.Config) { *c = config.Config{} })
}

// playingAt makes the player and the renderer agree that queue[i] is playing: in the
// run that starts at it (ADR-016) or, for a track the library lacks ("old"), alone.
func playingAt(r *testkit.Renderer, ids []string, i int, pos float64) {
	queue, index, wantPlaying, position = ids, i, true, pos
	lastCmd = time.Now().Add(-time.Minute)
	uri := "http://x/" + ids[i]
	if rp, err := planRun(r.Device, i, 0); err == nil {
		setRunLocked(rp)
		streamPos, duration = pos, rp.durs[0]
		uri = rp.uri
		r.SetURILength(uri, rp.length)
	}
	r.With(func(s *testkit.RendererState) {
		s.State, s.URI, s.Pos = "PLAYING", uri, pos
	})
}

// runOn plans the run from queue track i as the player would and tells the fake its length.
func runOn(t *testing.T, r *testkit.Renderer, i int, from float64) runPlan {
	t.Helper()
	rp, err := planRun(r.Device, i, from)
	if err != nil {
		t.Fatal(err)
	}
	r.SetURILength(rp.uri, rp.length)
	return rp
}

func TestStartFromStoppedSetsURIAndQueuesNext(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "b"}
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	st := r.Get()
	if st.State != "PLAYING" || st.URI != runOn(t, r, 0, 0).uri {
		t.Fatalf("state %s, uri %s", st.State, st.URI)
	}
	if st.Next != "" {
		t.Fatalf("next %q: the run holds the whole playlist, nothing comes after it", st.Next)
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
	if st := r.Get(); st.URI != runOn(t, r, 1, 0).uri || at(st) != 0 {
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

// silent reports whether a run serves silence now. The test tracks have no files, so
// a run that is still to be played cannot even be opened.
func silent(t *testing.T, uri string) bool {
	t.Helper()
	id, ok := runIDOf(uri)
	if !ok {
		t.Fatalf("not a run: %s", uri)
	}
	src, ok := stream.ForRun(id)
	if !ok {
		t.Fatalf("run %s is not registered", id)
	}
	rd, err := src.Open(services.WavHeaderLen)
	if err != nil {
		return false
	}
	defer func() { _ = rd.Close() }()
	buf := make([]byte, 64)
	n, _ := io.ReadFull(rd, buf)
	return n == len(buf) && bytes.Equal(buf, make([]byte, len(buf)))
}

// Moving the renderer off a stream through its gapless path plays the last second
// of that stream first: another song. The player lets that stream go first, so the
// renderer fetches silence there; the volume is not touched (ADR-003, ADR-016).
func TestSwitchSilencesTheOldStream(t *testing.T) {
	for _, state := range []string{"PLAYING", "PAUSED_PLAYBACK"} {
		t.Run(state, func(t *testing.T) {
			r := setup(t)
			playingAt(r, []string{"a", "hi"}, 0, 20)
			old := runURI
			r.With(func(s *testkit.RendererState) { s.State = state })
			if err := PlayIndex(1); err != nil {
				t.Fatal(err)
			}
			if !silent(t, old) || r.Count("SetVolume") != 0 {
				t.Fatalf("old stream silent %v, SetVolume %d", silent(t, old), r.Count("SetVolume"))
			}
			if st := r.Get(); st.URI != runURI || st.State != "PLAYING" {
				t.Fatalf("%+v", st)
			}
			// played again later, the same run is music again
			runOn(t, r, 0, 0)
			if silent(t, old) {
				t.Fatal("a run registered anew must not stay silent")
			}
		})
	}
}

func TestFailedSwitchKeepsTheOldStream(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "hi"}, 0, 20)
	old := runURI
	r.With(func(s *testkit.RendererState) { s.NoAdvance = true })
	if err := PlayIndex(1); err == nil {
		t.Fatal("a renderer that never switches must give an error")
	}
	if silent(t, old) {
		t.Fatal("the renderer stays on the old stream: it must not go silent")
	}
}

func TestSameTrackRestartsInPlace(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	uri := runOn(t, r, 0, 0).uri
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
	if st := r.Get(); st.State != "PLAYING" || st.URI != runOn(t, r, 1, 0).uri || index != 1 {
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
	if st.State != "PLAYING" || st.URI != runURI || at(st) != 26 || index != 1 {
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
	if err := PlayIndex(1); err != nil || Snapshot().Waiting == "" {
		t.Fatalf("a refused Play is retried, not reported at once: err %v", err)
	}
	r.Fail("Play", false)
	pendingSince = time.Time{}
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
	// the run opens at track 0 (the same format before the chosen track, ADR-020) and the
	// renderer is seeked past it to track 1
	rp, err := planStart(r.Device, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Get().URI != rp.uri || queueVersion != 1 || rp.q != 0 || at(r.Get()) != 0 || index != 1 {
		t.Fatalf("uri %s (want %s) version %d run from %d at %v index %d", r.Get().URI, rp.uri, queueVersion, rp.q, at(r.Get()), index)
	}
}

func TestSetQueueKeepsPlayingTrack(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 1, 10)
	SetQueue([]string{"c", "b", "a"}, "")
	if index != 1 {
		t.Fatalf("index %d, want the playing track found in the new list", index)
	}
	if r.Get().Next != runOn(t, r, 2, 0).uri {
		t.Fatalf("next %s: the run after the playing one, in the new playlist", r.Get().Next)
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
	halting.Wait()
	if r1.Count("Pause") != 1 || r1.Count("Stop") != 0 {
		t.Fatalf("old renderer: pause %d stop %d", r1.Count("Pause"), r1.Count("Stop"))
	}
	if st := r2.Get(); st.State != "PLAYING" || at(st) != 50 {
		t.Fatalf("new renderer %+v", st)
	}
	if config.Get().Renderer != "r2" {
		t.Fatalf("saved renderer %q", config.Get().Renderer)
	}
}

// A renderer left by a switch is only paused (the FiiO): it must hold nothing of the
// music for its next Play to let out first (the FiiO back from the BeagleBone,
// 2026-11-11). Its stream is let go and it is seeked in it.
func TestSelectAwayLeavesSilence(t *testing.T) {
	r1 := setup(t)
	r2 := newRenderer(t, "r2")
	renderers = append(renderers, r2.Device)
	playingAt(r1, []string{"a"}, 0, 50)
	old := runURI
	if err := Select("r2"); err != nil {
		t.Fatal(err)
	}
	halting.Wait()
	if r1.Count("Pause") != 1 || r1.Last("Seek")["Target"] != services.FormatSeekTarget(50) || !silent(t, old) {
		t.Fatalf("pause %d seek %v silent %v", r1.Count("Pause"), r1.Last("Seek"), silent(t, old))
	}
	if st := r2.Get(); st.State != "PLAYING" || at(st) != 50 || silent(t, runURI) {
		t.Fatalf("new renderer %+v", st)
	}
}

// The old renderer's state does not move to the new one, and an old renderer that
// is gone does not hold the start on the new one.
func TestSelectAwayFromDeadRenderer(t *testing.T) {
	r1 := setup(t)
	r2 := newRenderer(t, "r2")
	renderers = append(renderers, r2.Device)
	playingAt(r1, []string{"a"}, 0, 50)
	transport = "OFFLINE"
	// the old renderer takes the request and never answers
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-hang }))
	t.Cleanup(func() {
		close(hang)
		halting.Wait()
		srv.Close()
	})
	renderers[0].AVTransportURL = srv.URL + "/avt"
	start := time.Now()
	if err := Select("r2"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Select took %v: it waited for the hung renderer", d)
	}
	if st := r2.Get(); st.State != "PLAYING" || at(st) != 50 {
		t.Fatalf("new renderer %+v", st)
	}
	if transport == "OFFLINE" {
		t.Fatal("the old renderer's OFFLINE moved to the new one")
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
	if st := r2.Get(); st.State != "PLAYING" || at(st) != 50 || resumeAt != -1 {
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
	Tick()
	if got := Snapshot().Transport; got != "STOPPED" {
		t.Fatalf("the page shows %s after Stop, want STOPPED: the Pause is only how the FiiO is stopped", got)
	}
}

// Stop on a renderer that only pauses: it must hold nothing of the music for the next
// Play to let out, and the slider goes to the start, where the next Play begins.
func TestStopLeavesSilenceAndTheStart(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "b"}
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	r.With(func(s *testkit.RendererState) { s.Pos = 75 })
	Tick()
	old := runURI
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if !silent(t, old) || r.Last("Seek")["Target"] != services.FormatSeekTarget(75) {
		t.Fatalf("silent %v, seek %v", silent(t, old), r.Last("Seek"))
	}
	Tick()
	if s := Snapshot(); s.Position != 0 || s.Index != 0 {
		t.Fatalf("position %v index %d", s.Position, s.Index)
	}
	if err := Resume(); err != nil {
		t.Fatal(err)
	}
	if st := r.Get(); st.State != "PLAYING" || st.URI != runURI || at(st) != 0 || silent(t, runURI) {
		t.Fatalf("%+v: the track again from its start, music", st)
	}
}

func TestNextAndPrev(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 1, 10)
	if err := Next(); err != nil || r.Count("Seek") != 0 {
		t.Fatalf("end of queue: err %v seeks %d", err, r.Count("Seek"))
	}
	// deep into the track: still the previous track, as in foobar2000 (owner, 2026-11-11)
	if err := Prev(); err != nil {
		t.Fatal(err)
	}
	if index != 0 || at(r.Get()) != 0 {
		t.Fatalf("index %d at %v, want the previous track from its start", index, at(r.Get()))
	}
	// on the first track: back to its start
	position = 30
	if err := Prev(); err != nil {
		t.Fatal(err)
	}
	if index != 0 || at(r.Get()) != 0 {
		t.Fatalf("index %d at %v, want the first track from its start", index, at(r.Get()))
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
	playingAt(r, []string{"a", "hi", "c"}, 0, 239)
	queueNext(r.Device)
	r.With(func(s *testkit.RendererState) { s.URI, s.Pos = nextURL, 1 })
	Tick()
	if index != 1 || config.Get().Playlist.Index != 1 {
		t.Fatalf("index %d saved %d", index, config.Get().Playlist.Index)
	}
	if r.Get().Next != runOn(t, r, 2, 0).uri {
		t.Fatalf("next %s", r.Get().Next)
	}
	if transport != "PLAYING" || duration != 200 || position != 1 {
		t.Fatalf("transport %s duration %v position %v", transport, duration, position)
	}
}

func TestTickIgnoresEarlyURIChange(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "hi"}, 0, 100)
	queueNext(r.Device)
	// gmrender names next as current minutes early but still reports the old length
	r.SetURILength(nextURL, 240)
	r.With(func(s *testkit.RendererState) { s.URI = nextURL })
	Tick()
	if index != 0 {
		t.Fatalf("index %d: advanced on the early URI report", index)
	}
}

func TestTickStartsNextWhenStopped(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "hi"}, 0, 10)
	r.With(func(s *testkit.RendererState) { s.State, s.URI = "STOPPED", "" })
	Tick()
	if index != 1 || r.Get().URI != runOn(t, r, 1, 0).uri {
		t.Fatalf("index %d uri %s", index, r.Get().URI)
	}
}

func TestTickStartNextFailureIsNotRetriedEveryPoll(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "hi"}, 0, 10)
	r.With(func(s *testkit.RendererState) { s.State, s.URI = "STOPPED", "" })
	r.Fail("Play", true)
	Tick()
	Tick()
	Tick()
	if n := r.Count("Play"); n != playTries { // one start with its own retries, not one per poll
		t.Fatalf("Play sent %d times, want %d", n, playTries)
	}
	// the retries of a refused start are paced, and they end in an error
	pendingSince = time.Now().Add(-services.StartRetryFor)
	Tick()
	if wantPlaying || Snapshot().Error == "" || Snapshot().Waiting != "" {
		t.Fatalf("wantPlaying %v, error %q, waiting %q", wantPlaying, Snapshot().Error, Snapshot().Waiting)
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

// Every run is WAV decoded here (ADR-016): a new equalizer while playing is a seek to
// where it plays, which makes the renderer fetch the stream anew.
func TestSetEQReseeksWhilePlaying(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	if err := PlayIndex(0); err != nil {
		t.Fatal(err)
	}
	position = 30
	if err := SetEQ(eqOn()); err != nil {
		t.Fatal(err)
	}
	if !config.Get().EQ.Active() || !stream.EQActive() {
		t.Fatal("settings not applied")
	}
	if r.Get().Pos != 30 || r.Count("SetNextAVTransportURI") != 0 {
		t.Fatalf("pos %v setnext %d", r.Get().Pos, r.Count("SetNextAVTransportURI"))
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

func TestSetEQLeavesIdleAlone(t *testing.T) {
	r := setup(t)
	if err := SetEQ(eqOn()); err != nil {
		t.Fatal(err)
	}
	if len(r.Actions()) != 0 {
		t.Fatalf("idle renderer got %v", r.Actions())
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

// A run is WAV, of the tracks that follow in the same format, with the whole length in
// its DIDL and the first track's tags (ADR-016).
func TestRunLayout(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "b", "c"}
	rp := runOn(t, r, 0, 0)
	if !strings.Contains(rp.uri, "127.0.0.1:8090/media/run/") || !strings.Contains(rp.uri, ".wav?ct=audio%2Fx-wav") {
		t.Fatal(rp.uri)
	}
	if strings.Join(rp.ids, ",") != "a,b,c" || rp.length != 620 || rp.starts[1] != 240 || rp.starts[2] != 420 {
		t.Fatalf("ids %v length %v starts %v", rp.ids, rp.length, rp.starts)
	}
	if !strings.Contains(rp.meta, "/media/art/a") || !strings.Contains(rp.meta, `duration="0:10:20`) {
		t.Fatal(rp.meta)
	}
	if from := runOn(t, r, 1, 30); from.length != 150+200 || from.from != 30 || strings.Join(from.ids, ",") != "b,c" {
		t.Fatalf("from 30: %+v", from)
	}
}

func TestRunEndsWhereTheFormatChanges(t *testing.T) {
	r := setup(t)
	hi := *tracks["c"]
	hi.ID, hi.SampleRate, hi.End = "hi", 96000, 200*96000
	lookupTrack = func(id string) *library.Track {
		if id == "hi" {
			return &hi
		}
		return tracks[id]
	}
	queue = []string{"a", "b", "hi", "c"}
	if rp := runOn(t, r, 0, 0); strings.Join(rp.ids, ",") != "a,b" {
		t.Fatalf("run %v", rp.ids)
	}
	if rp := runOn(t, r, 2, 0); strings.Join(rp.ids, ",") != "hi" {
		t.Fatalf("run %v", rp.ids)
	}
}

// An update or a restart while paused (or playing) is not a Stop: Play continues the
// track at the same second, with no detour through its opening.
func TestResumeAfterRestartContinuesAtPosition(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a", "b"}, 1, 42)
	r.With(func(s *testkit.RendererState) { s.State = "PAUSED_PLAYBACK" })
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
	// the run opens at track a, before the paused one (ADR-020): one more seek takes the
	// renderer past it to b at 42 s
	if st.State != "PLAYING" || at(st) != 42 || r.Count("Seek") != 2 {
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

// stopSafe turns the fake into stock gmrender (the BeagleBone), which survives Stop.
func stopSafe(r *testkit.Renderer) {
	keepOutputOpen = false // driven with Stop, as before the experiment
	renderers[0].Model = "gmediarender"
	r.With(func(s *testkit.RendererState) { s.StopAllowed = true })
}

// On a renderer that survives Stop, a busy renderer is switched the way foobar2000
// does it: Stop -> SetAVTransportURI -> Play, not the next-URI path with a seek to the
// end (ADR-007).
func TestStopSafeRendererSwitchesLikeFoobar(t *testing.T) {
	r := setup(t)
	stopSafe(r)
	playingAt(r, []string{"a", "hi", "c"}, 0, 30)
	if err := PlayIndex(1); err != nil {
		t.Fatal(err)
	}
	st := r.Get()
	if r.Count("Stop") != 1 || r.Count("SetAVTransportURI") != 1 || r.Count("Seek") != 0 {
		t.Fatalf("stop %d setURI %d seek %d", r.Count("Stop"), r.Count("SetAVTransportURI"), r.Count("Seek"))
	}
	if st.State != "PLAYING" || st.URI != runOn(t, r, 1, 0).uri || index != 1 {
		t.Fatalf("%+v index %d", st, index)
	}
	// gapless playback stays: the following run is still queued as next
	if st.Next != runOn(t, r, 2, 0).uri {
		t.Fatalf("next %q", st.Next)
	}
}

// Stop and switching away really stop such a renderer, so it does not sit paused with
// an idle pipeline; the FiiO keeps getting Pause (TestPauseAndStopNeverSendStop).
func TestStopSafeRendererIsStoppedNotPaused(t *testing.T) {
	r := setup(t)
	stopSafe(r)
	playingAt(r, []string{"a"}, 0, 30)
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if r.Count("Stop") != 1 || r.Count("Pause") != 0 || r.Get().State != "STOPPED" {
		t.Fatalf("stop %d pause %d state %s", r.Count("Stop"), r.Count("Pause"), r.Get().State)
	}

	r2 := newRenderer(t, "r2")
	renderers = append(renderers, r2.Device)
	playingAt(r, []string{"a"}, 0, 30)
	if err := Select("r2"); err != nil {
		t.Fatal(err)
	}
	if r.Count("Stop") != 2 || r.Count("Pause") != 0 {
		t.Fatalf("leaving it: stop %d pause %d", r.Count("Stop"), r.Count("Pause"))
	}
}

// The BeagleBone answered "501 Playing failed" to a Play sent right after Stop
// (2026-11-10); a moment later it plays. Switching back to it must still land at the
// position the track had on the other renderer.
func TestStopSafeRendererPlayRetriedAfterStop(t *testing.T) {
	r1 := setup(t)
	stopSafe(r1)
	r2 := newRenderer(t, "r2")
	renderers = append(renderers, r2.Device)
	selected = "r2"
	playingAt(r2, []string{"a"}, 0, 50)
	r1.With(func(s *testkit.RendererState) { s.State, s.URI = "PAUSED_PLAYBACK", "http://x/old" })
	r1.FailNext("Play", 2)
	if err := Select("r1"); err != nil {
		t.Fatal(err)
	}
	if st := r1.Get(); st.State != "PLAYING" || at(st) != 50 || r1.Count("Play") != 3 {
		t.Fatalf("%+v plays %d", st, r1.Count("Play"))
	}
}

// When the start fails anyway, the wanted position is kept: the next Play starts the
// track there instead of from 0.
func TestFailedStartKeepsPositionForNextPlay(t *testing.T) {
	r1 := setup(t)
	r2 := newRenderer(t, "r2")
	renderers = append(renderers, r2.Device)
	playingAt(r1, []string{"a"}, 0, 50)
	r2.Fail("SetAVTransportURI", true)
	if err := Select("r2"); err == nil {
		t.Fatal("want the failed start reported")
	}
	if resumeAt != 50 {
		t.Fatalf("resumeAt %v", resumeAt)
	}
	r2.Fail("SetAVTransportURI", false)
	if err := Resume(); err != nil {
		t.Fatal(err)
	}
	if st := r2.Get(); st.State != "PLAYING" || at(st) != 50 {
		t.Fatalf("%+v", st)
	}
}

// The BeagleBone refuses to play while the amplifier's input is still on coaxial. The
// player does not report that at once: it says on the page that it is retrying, and
// tries again every couple of seconds; once the owner switches the input, the track
// plays from the position it was asked for (ADR-014).
func TestRefusedStartIsRetriedAndSaysSo(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	r.Fail("Play", true)
	if err := startLocked(0, 40); err != nil {
		t.Fatal(err)
	}
	s := Snapshot()
	if s.Error != "" || !strings.Contains(s.Waiting, "Connecting to r1: attempt 1") || s.Position != 40 {
		t.Fatalf("error %q waiting %q position %v", s.Error, s.Waiting, s.Position)
	}
	plays := r.Count("Play")
	Tick() // too soon: no new attempt
	if r.Count("Play") != plays {
		t.Fatal("retried before services.StartRetryEvery")
	}
	pendingTry = time.Now().Add(-services.StartRetryEvery)
	Tick() // refused again
	if !strings.Contains(Snapshot().Waiting, "attempt 2") {
		t.Fatalf("waiting %q", Snapshot().Waiting)
	}
	r.Fail("Play", false) // the input is switched to USB now
	pendingTry = time.Now().Add(-services.StartRetryEvery)
	Tick()
	if st := r.Get(); st.State != "PLAYING" || at(st) != 40 || Snapshot().Waiting != "" || !wantPlaying {
		t.Fatalf("%+v waiting %q", st, Snapshot().Waiting)
	}
}

// Stop ends the retries; Pause ends them too but keeps the position.
func TestRefusedStartRetriesEndOnStopAndPause(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	r.Fail("Play", true)
	_ = startLocked(0, 40)
	if err := Pause(); err != nil {
		t.Fatal(err)
	}
	if !pendingSince.IsZero() || resumeAt != 40 || !paused {
		t.Fatalf("pending %v resumeAt %v paused %v", pendingSince, resumeAt, paused)
	}
	_ = startLocked(0, 40)
	_ = Stop()
	if !pendingSince.IsZero() || wantPlaying || Snapshot().Waiting != "" {
		t.Fatalf("pending %v want %v", pendingSince, wantPlaying)
	}
}

// A start at a position is a stream that begins there: the renderer plays it from its
// start and is never seeked, so the opening of the track is not heard (ADR-015). The
// player shifts the renderer's position and length back by where the stream starts.
func TestStartAtPositionIsAStreamFromThere(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "b"}
	if err := startLocked(0, 50); err != nil {
		t.Fatal(err)
	}
	st := r.Get()
	if r.Count("Seek") != 0 || st.URI != runOn(t, r, 0, 50).uri {
		t.Fatalf("seeks %d, uri %s", r.Count("Seek"), st.URI)
	}
	r.With(func(s *testkit.RendererState) { s.Pos = 10 })
	Tick()
	if s := Snapshot(); s.Position != 60 || s.Duration != 240 || s.Index != 0 {
		t.Fatalf("position %v duration %v index %d", s.Position, s.Duration, s.Index)
	}
	// the same stream goes on into the next track: 190 s into it is b at 0
	r.With(func(s *testkit.RendererState) { s.Pos = 195 })
	Tick()
	if s := Snapshot(); s.Position != 5 || s.Duration != 180 || s.Index != 1 || config.Get().Playlist.Index != 1 {
		t.Fatalf("position %v duration %v index %d saved %d", s.Position, s.Duration, s.Index, config.Get().Playlist.Index)
	}
}

// Seeking inside such a stream is relative to its start; seeking before its start
// starts the track again from there.
func TestSeekWithAStreamFromAPosition(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	if err := startLocked(0, 50); err != nil {
		t.Fatal(err)
	}
	if err := Seek(70); err != nil {
		t.Fatal(err)
	}
	if got := r.Last("Seek")["Target"]; got != services.FormatSeekTarget(20) || position != 70 {
		t.Fatalf("target %s position %v", got, position)
	}
	if err := Seek(30); err != nil {
		t.Fatal(err)
	}
	if st := r.Get(); st.URI != runOn(t, r, 0, 30).uri || at(st) != 30 || offset != 30 {
		t.Fatalf("%+v offset %v", st, offset)
	}
	if err := Seek(0); err != nil { // back to the very start: a plain stream again
		t.Fatal(err)
	}
	if st := r.Get(); st.URI != runOn(t, r, 0, 0).uri || offset != 0 {
		t.Fatalf("%+v offset %v", st, offset)
	}
}

// The renderer moving on from such a stream to the next track is an advance like
// any other; the next track's stream starts at 0 again.
func TestAdvanceFromAStreamFromAPosition(t *testing.T) {
	r := setup(t)
	queue = []string{"a", "hi"}
	if err := startLocked(0, 50); err != nil {
		t.Fatal(err)
	}
	next := r.Get().Next
	if next != runOn(t, r, 1, 0).uri {
		t.Fatalf("next %q", next)
	}
	r.With(func(s *testkit.RendererState) { s.URI, s.Next, s.Pos = next, "", 1 })
	Tick()
	if s := Snapshot(); s.Index != 1 || s.Position != 1 || s.Duration != 200 || offset != 0 {
		t.Fatalf("index %d position %v duration %v offset %v", s.Index, s.Position, s.Duration, offset)
	}
}

// The FiiO SR11 stays in PLAYING at the very end after the last track. The playlist
// has ended once the position reaches the track's known length: the player finishes
// and pauses it, so the page shows neither STALLED nor PLAYING (2026-11-10).
func TestEndOfPlaylistWithRendererFrozenAtTheEnd(t *testing.T) {
	r := setup(t)
	playingAt(r, []string{"a"}, 0, 240)
	Tick()
	if r.Count("Pause") != 0 {
		t.Fatal("paused right at the end: the last fraction of a second would be cut")
	}
	finishedAt = time.Now().Add(-2 * time.Second)
	Tick()
	s := Snapshot()
	if s.Stalled || wantPlaying || r.Count("Pause") != 1 || r.Get().State != "PAUSED_PLAYBACK" {
		t.Fatalf("stalled %v want %v pauses %d state %s", s.Stalled, wantPlaying, r.Count("Pause"), r.Get().State)
	}
}

// The live case of 2026-11-10: a stream that starts half a second before the end of
// the last track. The renderer reports no length for it and sits in PLAYING at 0.
// That is the end of the playlist, not a hang.
func TestEndOfPlaylistOnAStreamFromNearTheEnd(t *testing.T) {
	r := setup(t)
	queue, index, wantPlaying, duration = []string{"a"}, 0, true, 240
	lastCmd = time.Now().Add(-time.Minute)
	rp := runOn(t, r, 0, 239.5)
	setRunLocked(rp)
	r.SetURILength(rp.uri, 0) // what the renderer says about a half-second stream
	r.With(func(s *testkit.RendererState) { s.State, s.URI, s.Pos = "PLAYING", rp.uri, 0 })
	Tick()
	if s := Snapshot(); s.Stalled || wantPlaying {
		t.Fatalf("stalled %v want %v", s.Stalled, wantPlaying)
	}
}

// A start within 2 s of the end is a start of a track that has already ended: it
// starts the track over instead of a stream of the last half second.
func TestStartAtTheVeryEndStartsOver(t *testing.T) {
	r := setup(t)
	queue = []string{"a"}
	if err := startLocked(0, 239.5); err != nil {
		t.Fatal(err)
	}
	if st := r.Get(); st.URI != runOn(t, r, 0, 0).uri || offset != 0 {
		t.Fatalf("uri %s offset %v", st.URI, offset)
	}
}
