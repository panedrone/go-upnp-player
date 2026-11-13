package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/handlers"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
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

type client struct {
	t   *testing.T
	app *fiber.App
}

func (c client) do(method, path, body string, hdr ...string) (*http.Response, []byte) {
	c.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp, data
}

// status sends a request and checks the status code.
func (c client) status(want int, method, path, body string) []byte {
	c.t.Helper()
	resp, data := c.do(method, path, body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, data)
	}
	return data
}

func (c client) state() player.State {
	c.t.Helper()
	var s player.State
	if err := json.Unmarshal(c.status(200, "GET", "/api/state", ""), &s); err != nil {
		c.t.Fatal(err)
	}
	return s
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 500 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type trackDTO struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	FromCue  bool    `json:"fromCue"`
}

type nodeDTO struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// makeLibrary lays out: album/ (image + cue with two tracks + cover.jpg) and a
// tagged loose.flac; about 11 s of audio each.
func makeLibrary(t *testing.T) (root string, cover []byte) {
	t.Helper()
	root = t.TempDir()
	album := filepath.Join(root, "album")
	if err := os.Mkdir(album, 0o755); err != nil {
		t.Fatal(err)
	}
	long := testkit.FLAC{Frames: 120, BlockSize: 4096}
	testkit.WriteFLAC(t, filepath.Join(album, "image.flac"), long)
	cue := "PERFORMER \"Band\"\nTITLE \"Record\"\nFILE \"image.wav\" WAVE\n" +
		"  TRACK 01 AUDIO\n    TITLE \"One\"\n    INDEX 01 00:00:00\n" +
		"  TRACK 02 AUDIO\n    TITLE \"Two\"\n    INDEX 01 00:05:00\n"
	if err := os.WriteFile(filepath.Join(album, "image.cue"), []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	cover = []byte("jpeg bytes")
	if err := os.WriteFile(filepath.Join(album, "cover.jpg"), cover, 0o644); err != nil {
		t.Fatal(err)
	}
	long.Tags = [][2]string{{"TITLE", "Loose Song"}, {"ARTIST", "Solo"}}
	testkit.WriteFLAC(t, filepath.Join(root, "loose.flac"), long)
	return root, cover
}

// The whole API in the order a user meets it. Player and library are one per
// process, so this is one scenario rather than independent tests; only the
// renderer and the music files are fake.
func TestEndpointsScenario(t *testing.T) {
	r := testkit.NewRenderer(t, "uuid:fake")
	player.DiscoverRenderers = func(context.Context, time.Duration) ([]upnp.Device, error) {
		return []upnp.Device{r.Device}, nil
	}
	t.Cleanup(func() { player.DiscoverRenderers = upnp.Discover })
	root, cover := makeLibrary(t)
	// config and library are per process: leave nothing from a previous -count run
	if err := config.Update(func(c *config.Config) { *c = config.Config{} }); err != nil {
		t.Fatal(err)
	}
	library.Scan(nil)

	handlers.BuildID, handlers.AppVersion, player.Build = "b1", "9.9", "b1"
	player.Init(8090)
	app := fiber.New()
	InitRoutes(app)
	c := client{t, app}

	var cueTracks []trackDTO
	var loose trackDTO

	t.Run("version and empty settings", func(t *testing.T) {
		if got := string(c.status(200, "GET", "/api/version", "")); !strings.Contains(got, `"build":"b1"`) || !strings.Contains(got, `"version":"9.9"`) {
			t.Fatal(got)
		}
		if got := string(c.status(200, "GET", "/api/settings", "")); got != `{"roots":[]}` {
			t.Fatal(got)
		}
		if got := string(c.status(200, "GET", "/api/tree", "")); got != "null" && got != "[]" {
			t.Fatal(got)
		}
	})

	t.Run("settings start a scan", func(t *testing.T) {
		c.status(400, "PUT", "/api/settings", "{broken")
		body, _ := json.Marshal(map[string]any{"roots": []string{root}})
		c.status(204, "PUT", "/api/settings", string(body))
		waitFor(t, "the library scan", func() bool {
			s := c.state().Library
			return !s.Scanning && s.Tracks == 4 // 2 cue tracks + covered image + loose file
		})
		if got := string(c.status(200, "GET", "/api/settings", "")); !strings.Contains(got, "roots") || !strings.Contains(got, "album") && !strings.Contains(got, filepath.Base(root)) {
			t.Fatal(got)
		}
		c.status(202, "POST", "/api/library/rescan", "")
	})

	t.Run("tree, tracks, search", func(t *testing.T) {
		var nodes []nodeDTO
		mustJSON(t, c.status(200, "GET", "/api/tree", ""), &nodes)
		if len(nodes) != 1 || nodes[0].Kind != "dir" {
			t.Fatalf("%+v", nodes)
		}
		mustJSON(t, c.status(200, "GET", "/api/tree?path="+esc(root), ""), &nodes)
		if len(nodes) != 2 || nodes[0].Name != "album" || nodes[1].Kind != "file" {
			t.Fatalf("%+v", nodes)
		}
		mustJSON(t, c.status(200, "GET", "/api/tree?path="+esc(filepath.Join(root, "album")), ""), &nodes)
		if len(nodes) != 2 || nodes[0].Kind != "cue" || nodes[1].Name != "image.flac" {
			t.Fatalf("%+v", nodes)
		}
		mustJSON(t, c.status(200, "GET", "/api/tree?path="+esc(filepath.Join(root, "album", "image.cue")), ""), &nodes)
		if len(nodes) != 2 || nodes[1].Name != "Two" {
			t.Fatalf("%+v", nodes)
		}
		var list []trackDTO
		mustJSON(t, c.status(200, "GET", "/api/tracks?path="+esc(root), ""), &list)
		if len(list) != 3 { // the covered image does not appear next to its cue tracks
			t.Fatalf("%+v", list)
		}
		for _, tr := range list {
			if tr.FromCue {
				cueTracks = append(cueTracks, tr)
			} else {
				loose = tr
			}
		}
		if len(cueTracks) != 2 || loose.Title != "Loose Song" {
			t.Fatalf("cue %+v loose %+v", cueTracks, loose)
		}
		mustJSON(t, c.status(200, "GET", "/api/search?q=solo", ""), &list)
		if len(list) != 1 || list[0].ID != loose.ID {
			t.Fatalf("%+v", list)
		}
		if got := string(c.status(200, "GET", "/api/search?q=", "")); got != "[]" {
			t.Fatal(got)
		}
		if got := string(c.status(200, "GET", "/api/tracks?path=/nowhere", "")); got != "[]" {
			t.Fatal(got)
		}
		if got := string(c.status(200, "GET", "/api/tree?path=/nowhere", "")); got != "[]" {
			t.Fatal(got)
		}
		if got := string(c.status(200, "GET", "/api/track/"+cueTracks[1].ID+"/properties", "")); !strings.Contains(got, `"number":2`) || !strings.Contains(got, `"codec":"FLAC"`) {
			t.Fatal(got)
		}
		c.status(404, "GET", "/api/track/nope/properties", "")
		r.SetLength(cueTracks[0].ID, cueTracks[0].Duration)
		r.SetLength(cueTracks[1].ID, cueTracks[1].Duration)
		r.SetLength(loose.ID, loose.Duration)
	})

	t.Run("commands without a renderer fail with 502", func(t *testing.T) {
		c.status(502, "POST", "/api/player/pause", "")
	})

	t.Run("discovery", func(t *testing.T) {
		c.status(202, "POST", "/api/renderers/refresh", "")
		waitFor(t, "the renderer", func() bool {
			s := c.state()
			return len(s.Renderers) == 1 && s.Selected == "uuid:fake" && !s.Discovering
		})
	})

	t.Run("playlist does not touch playback", func(t *testing.T) {
		c.status(400, "PUT", "/api/queue", "{broken")
		c.status(204, "PUT", "/api/queue", fmt.Sprintf(`{"ids":[%q,%q,%q]}`, cueTracks[0].ID, cueTracks[1].ID, loose.ID))
		var q struct {
			Version int        `json:"version"`
			Tracks  []trackDTO `json:"tracks"`
		}
		mustJSON(t, c.status(200, "GET", "/api/queue", ""), &q)
		if len(q.Tracks) != 3 || q.Version == 0 {
			t.Fatalf("%+v", q)
		}
		if r.Count("Play") != 0 {
			t.Fatal("building a playlist started playback")
		}
	})

	t.Run("play", func(t *testing.T) {
		c.status(400, "POST", "/api/player/play", "{broken")
		c.status(502, "POST", "/api/player/play", `{"index":99}`)
		c.status(204, "POST", "/api/player/resume", "")
		st := r.Get()
		if st.State != "PLAYING" || !strings.Contains(st.URI, "/media/run/") {
			t.Fatalf("%+v", st)
		}
		s := c.state()
		if s.Track == nil || s.Track.ID != cueTracks[0].ID || !s.WantPlaying {
			t.Fatalf("%+v", s)
		}
		c.status(204, "POST", "/api/player/play", fmt.Sprintf(`{"ids":[%q,%q],"index":0}`, cueTracks[0].ID, cueTracks[1].ID))
		run := r.Get().URI
		if !strings.Contains(run, "/media/run/") {
			t.Fatal(run)
		}
		// one WAV of both tracks, back to back: the renderer never switches streams
		// between them (ADR-016)
		path := run[strings.Index(run, "/media/"):]
		_, whole := c.do("GET", path, "")
		_, one := c.do("GET", "/media/track/"+cueTracks[0].ID+".wav", "")
		_, two := c.do("GET", "/media/track/"+cueTracks[1].ID+".wav", "")
		// every stream's first 10 ms rise out of silence, the run's seam does not
		const fade = 441 * 4
		seam := len(one) // the second track's data starts here in the run
		if string(whole[:4]) != "RIFF" || len(whole) != len(one)+len(two)-44 ||
			!bytes.Equal(whole[44+fade:seam], one[44+fade:]) || !bytes.Equal(whole[seam+fade:], two[44+fade:]) ||
			bytes.Equal(whole[seam:seam+fade], two[44:44+fade]) {
			t.Fatalf("run %d bytes, tracks %d + %d", len(whole), len(one), len(two))
		}
		resp, part := c.do("GET", path, "", "Range", fmt.Sprintf("bytes=%d-%d", seam-4000, seam+3999))
		if resp.StatusCode != 206 || len(part) != 8000 || !bytes.Equal(part[fade:], whole[seam-4000+fade:seam+4000]) {
			t.Fatalf("%d, a range across the seam: %d bytes", resp.StatusCode, len(part))
		}
		c.status(204, "POST", "/api/player/play", `{"index":1}`)
		waitFor(t, "the switch", func() bool { return c.state().Index == 1 })
		if r.Get().URI != run || r.Last("Seek")["Target"] != "0:00:05" {
			t.Fatalf("%s, seek %s: a track of the stream is reached by a seek", r.Get().URI, r.Last("Seek")["Target"])
		}
	})

	t.Run("transport", func(t *testing.T) {
		c.status(204, "POST", "/api/player/pause", "")
		if r.Get().State != "PAUSED_PLAYBACK" || !c.state().Paused {
			t.Fatal("not paused")
		}
		c.status(204, "POST", "/api/player/resume", "")
		if r.Get().State != "PLAYING" {
			t.Fatal("not resumed")
		}
		c.status(400, "POST", "/api/player/seek", "{broken")
		c.status(204, "POST", "/api/player/seek", `{"value":3}`)
		// 3 s into the second track of the run: 5 + 3 s into the stream
		if r.Get().Pos != 8 || c.state().Position != 3 {
			t.Fatal(r.Get().Pos, c.state().Position)
		}
		c.status(400, "POST", "/api/player/volume", "{broken")
		c.status(204, "POST", "/api/player/volume", `{"value":55}`)
		if r.Get().Volume != 55 {
			t.Fatal(r.Get().Volume)
		}
		c.status(204, "POST", "/api/player/next", "") // already last
		c.status(204, "POST", "/api/player/prev", "") // past 3 s... restarts or steps back
		r.Fail("Pause", true)
		if got := c.status(502, "POST", "/api/player/pause", ""); !strings.Contains(string(got), "501") {
			t.Fatal(string(got))
		}
		r.Fail("Pause", false)
		c.status(204, "POST", "/api/player/stop", "")
		if c.state().WantPlaying || r.Count("Stop") != 0 {
			t.Fatal("stop must be a Pause plus a cleared intent")
		}
	})

	t.Run("renderer selection", func(t *testing.T) {
		c.status(400, "POST", "/api/renderers/select", "{broken")
		c.status(204, "POST", "/api/renderers/select", `{"udn":"uuid:fake"}`)
	})

	t.Run("media: virtual WAV", func(t *testing.T) {
		path := "/media/track/" + cueTracks[0].ID + ".wav?ct=audio/x-wav"
		resp, body := c.do("GET", path, "")
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/x-wav" || resp.Header.Get("Accept-Ranges") != "bytes" {
			t.Fatalf("%d %v", resp.StatusCode, resp.Header)
		}
		if string(body[:4]) != "RIFF" {
			t.Fatalf("no WAV header: %q", body[:4])
		}
		if want := 44 + 5*44100*4; len(body) != want {
			t.Fatalf("%d bytes, want %d", len(body), want)
		}
		// a Range is a stream of its own: its first 10 ms rise out of silence
		resp, part := c.do("GET", path, "", "Range", "bytes=20000-29999")
		if resp.StatusCode != 206 || len(part) != 10000 || !bytes.Equal(part[441*4:], body[20000+441*4:30000]) {
			t.Fatalf("%d, %d bytes", resp.StatusCode, len(part))
		}
		if got := resp.Header.Get("Content-Range"); got != fmt.Sprintf("bytes 20000-29999/%d", len(body)) {
			t.Fatal(got)
		}
		resp, _ = c.do("GET", path, "", "Range", "bytes=999999999-")
		if resp.StatusCode != 416 {
			t.Fatal(resp.StatusCode)
		}
		resp, head := c.do("HEAD", path, "")
		if resp.StatusCode != 200 || len(head) != 0 || resp.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
			t.Fatalf("%d, %d bytes, %s", resp.StatusCode, len(head), resp.Header.Get("Content-Length"))
		}
		resp, def := c.do("HEAD", "/media/track/"+cueTracks[0].ID+".wav", "")
		if resp.Header.Get("Content-Type") != "application/octet-stream" || len(def) != 0 {
			t.Fatal(resp.Header.Get("Content-Type"))
		}
	})

	t.Run("media: raw FLAC, the second track starts where the first ends", func(t *testing.T) {
		want, err := os.ReadFile(filepath.Join(root, "loose.flac"))
		if err != nil {
			t.Fatal(err)
		}
		_, body := c.do("GET", "/media/track/"+loose.ID+".flac?ct=audio/flac", "")
		if !bytes.Equal(body, want) {
			t.Fatalf("%d bytes, file is %d", len(body), len(want))
		}
		resp, _ := c.do("GET", "/media/track/nope.wav", "")
		if resp.StatusCode != 404 {
			t.Fatal(resp.StatusCode)
		}
		_, one := c.do("GET", "/media/track/"+cueTracks[0].ID+".wav", "")
		_, two := c.do("GET", "/media/track/"+cueTracks[1].ID+".wav", "")
		if len(two) <= 44 || bytes.Equal(one[44:1000], two[44:1000]) {
			t.Fatal("the tracks must be different ranges of the image")
		}
	})

	t.Run("art", func(t *testing.T) {
		resp, body := c.do("GET", "/media/art/"+cueTracks[0].ID, "")
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || !bytes.Equal(body, cover) {
			t.Fatalf("%d %q", resp.StatusCode, body)
		}
		if resp, _ = c.do("GET", "/media/art/"+loose.ID, ""); resp.StatusCode != 404 {
			t.Fatal(resp.StatusCode)
		}
		if resp, _ = c.do("GET", "/media/art/nope", ""); resp.StatusCode != 404 {
			t.Fatal(resp.StatusCode)
		}
	})

	t.Run("equalizer", func(t *testing.T) {
		var eq struct {
			Enabled bool      `json:"enabled"`
			Gains   []float64 `json:"gains"`
			Bands   []float64 `json:"bands"`
		}
		mustJSON(t, c.status(200, "GET", "/api/eq", ""), &eq)
		if len(eq.Bands) != 18 || len(eq.Gains) != 18 || eq.Enabled {
			t.Fatalf("%+v", eq)
		}
		_, flat := c.do("GET", "/media/track/"+loose.ID+".wav", "")
		eq.Enabled, eq.Gains[5] = true, 12
		body, _ := json.Marshal(eq)
		c.status(400, "PUT", "/api/eq", "{broken")
		c.status(204, "PUT", "/api/eq", string(body))
		if !stream.EQActive() {
			t.Fatal("EQ not applied")
		}
		_, boosted := c.do("GET", "/media/track/"+loose.ID+".wav", "")
		if len(boosted) != len(flat) || bytes.Equal(boosted, flat) {
			t.Fatal("the EQ must change samples and keep the length")
		}
		eq.Enabled = false
		body, _ = json.Marshal(eq)
		c.status(204, "PUT", "/api/eq", string(body))
	})

	t.Run("embedded UI", func(t *testing.T) {
		resp, body := c.do("GET", "/", "")
		if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-cache" || !strings.Contains(string(body), "<div") {
			t.Fatalf("%d %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
		// embedded files have a zero modification time: a conditional request
		// must still get the current page, or an update never reaches the browser
		resp, cond := c.do("GET", "/", "", "If-Modified-Since", "Mon, 09 Nov 2026 01:00:00 GMT")
		if resp.StatusCode != 200 || !bytes.Equal(cond, body) {
			t.Fatalf("%d, %d bytes", resp.StatusCode, len(cond))
		}
		resp, body = c.do("GET", "/assets/index-oldbuild.js", "")
		if resp.StatusCode != 200 || !strings.Contains(string(body), "location.reload") || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", resp.StatusCode, body)
		}
		if resp, _ = c.do("GET", "/no/such/file.png", ""); resp.StatusCode != 404 {
			t.Fatal(resp.StatusCode)
		}
	})

	t.Run("shutdown", func(t *testing.T) {
		handlers.RequestShutdown = nil
		c.status(503, "POST", "/api/shutdown", "")
		done := make(chan struct{})
		handlers.RequestShutdown = func() { close(done) }
		t.Cleanup(func() { handlers.RequestShutdown = nil })
		c.status(202, "POST", "/api/shutdown", "")
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("shutdown was not requested")
		}
	})

	t.Run("window", func(t *testing.T) {
		handlers.ShowWindow = nil
		c.status(404, "POST", "/api/window/show", "") // a player in the browser
		handlers.ShowWindow = func() bool { return true }
		t.Cleanup(func() { handlers.ShowWindow = nil })
		c.status(204, "POST", "/api/window/show", "")
	})
}

func TestRunningBuild(t *testing.T) {
	state := `{"renderers":[],"library":{"tracks":0}}`
	version, code := `{"build":"abc","version":"1.1.17"}`, 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/state":
			_, _ = io.WriteString(w, state)
		case "/api/version":
			w.WriteHeader(code)
			_, _ = io.WriteString(w, version)
		}
	}))
	defer srv.Close()
	ui := srv.URL + "/"
	if ok, b, v := RunningBuild(ui); !ok || b != "abc" || v != "1.1.17" {
		t.Fatalf("%v %q %q", ok, b, v)
	}
	code = 404 // a build from before /api/version
	if ok, b, v := RunningBuild(ui); !ok || b != "" || v != "" {
		t.Fatalf("%v %q %q", ok, b, v)
	}
	state = `{"something":"else"}`
	if ok, _, _ := RunningBuild(ui); ok {
		t.Fatal("some other program is not our player")
	}
	if alreadyRunning("http://127.0.0.1:1/") {
		t.Fatal("nothing listens there")
	}
}

func TestTakeOverAsksToQuitAndBinds(t *testing.T) {
	asked := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked <- r.Method + " " + r.URL.Path
	}))
	defer srv.Close()
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	ln, err := TakeOver(srv.URL+"/", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	if got := <-asked; got != "POST /api/shutdown" {
		t.Fatal(got)
	}
}

func TestBuildIDIsStable(t *testing.T) {
	a, b := BuildID(), BuildID()
	if len(a) != 12 || a != b {
		t.Fatalf("%q %q", a, b)
	}
}

func mustJSON(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
}

func esc(p string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", "\\", "%5C", ":", "%3A").Replace(p)
}
