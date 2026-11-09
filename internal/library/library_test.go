package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/panedrone/go-upnp-player/internal/testkit"
)

func TestMain(m *testing.M) {
	cleanup := testkit.IsolateUserDirs()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// reads counts header parses; the files themselves are empty stubs.
type fakeFiles struct {
	mu    sync.Mutex
	reads int
	tags  map[string]map[string]string // by base name
	bad   map[string]bool
}

// setup resets the library and replaces FLAC parsing: 100 s, 44.1 kHz stereo.
func setup(t *testing.T) *fakeFiles {
	t.Helper()
	reset()
	ff := &fakeFiles{tags: map[string]map[string]string{}, bad: map[string]bool{}}
	orig := readFlacMeta
	readFlacMeta = func(path string, st os.FileInfo) (*FileMeta, error) {
		ff.mu.Lock()
		defer ff.mu.Unlock()
		ff.reads++
		b := filepath.Base(path)
		if ff.bad[b] {
			return nil, errors.New("broken")
		}
		n := uint64(100 * 44100)
		if b == "unknown.flac" {
			n = 0
		}
		return &FileMeta{ModTime: st.ModTime().UnixNano(), Size: st.Size(), SampleRate: 44100, Channels: 2, Bits: 16, NSamples: n, Tags: ff.tags[b]}, nil
	}
	t.Cleanup(func() { readFlacMeta = orig; reset() })
	return ff
}

func reset() {
	cur = &index{byID: map[string]*Track{}, children: map[string][]Node{}}
	metaCache, cacheLoaded = map[string]*FileMeta{}, false
	artCache = map[string]string{}
	if p := cachePath(); p != "" {
		_ = os.Remove(p)
		_ = os.Remove(legacyCachePath())
	}
}

func touch(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const twoTracks = "PERFORMER \"Band\"\nTITLE \"Record\"\nFILE \"a.wav\" WAVE\n  TRACK 01 AUDIO\n    TITLE \"One\"\n    INDEX 01 00:00:00\n  TRACK 02 AUDIO\n    TITLE \"Two\"\n    INDEX 01 01:00:00\n"

func names(ns []Node) string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Kind+":"+n.Name)
	}
	return strings.Join(out, " ")
}

func TestScanBuildsTree(t *testing.T) {
	setup(t)
	root := t.TempDir()
	touch(t, root, map[string]string{
		"album/a.flac": "x", "album/a.cue": twoTracks, "album/notes.txt": "x",
		"Zed.flac": "x", "b/deep/c.FLAC": "x", "empty/readme.txt": "x",
	})
	Scan([]string{root, filepath.Join(root, "absent")})

	if got := names(Children("")); got != "dir:"+root {
		t.Fatal(got)
	}
	if got := names(Children(root)); got != "dir:album dir:b file:Zed.flac" {
		t.Fatal(got)
	}
	album := filepath.Join(root, "album")
	if got := names(Children(album)); got != "cue:a.cue file:a.flac" {
		t.Fatal(got)
	}
	tr := Children(filepath.Join(album, "a.cue"))
	if names(tr) != "track:One track:Two" || tr[1].Number != 2 || tr[1].Duration != 40 {
		t.Fatalf("%+v", tr)
	}
	if st := GetStatus(); st.Tracks != 5 || st.Scanning {
		t.Fatalf("%+v", st)
	}
	if Children(filepath.Join(root, "nowhere")) != nil {
		t.Fatal("an unknown node has no children")
	}
}

func TestCoveredFileIsNotDoubled(t *testing.T) {
	setup(t)
	root := t.TempDir()
	touch(t, root, map[string]string{"a.flac": "x", "a.cue": twoTracks})
	Scan([]string{root})
	if got := TracksUnder(root); len(got) != 2 || !got[0].FromCue {
		t.Fatalf("%d tracks under the folder", len(got))
	}
	// the file itself still plays when asked for directly
	got := TracksUnder(filepath.Join(root, "a.flac"))
	if len(got) != 1 || !got[0].Covered || got[0].FromCue {
		t.Fatalf("%+v", got)
	}
	if got := Search("record", 10); len(got) != 2 {
		t.Fatalf("%d found", len(got))
	}
}

func TestSearch(t *testing.T) {
	ff := setup(t)
	ff.tags["x.flac"] = map[string]string{"ARTIST": "Miles Davis", "TITLE": "So What", "ALBUM": "Kind of Blue"}
	ff.tags["y.flac"] = map[string]string{"ARTIST": "Davis Group", "TITLE": "Other"}
	root := t.TempDir()
	touch(t, root, map[string]string{"x.flac": "x", "y.flac": "x"})
	Scan([]string{root})
	if got := Search("DAVIS blue", 10); len(got) != 1 || got[0].Title != "So What" {
		t.Fatalf("%+v", got)
	}
	if got := Search("davis", 1); len(got) != 1 {
		t.Fatalf("limit ignored: %d", len(got))
	}
	if Search("   ", 10) != nil || Search("nothing here", 10) != nil {
		t.Fatal("no results expected")
	}
	id := Search("so what", 1)[0].ID
	if Get(id) == nil || Get("nope") != nil {
		t.Fatal("Get by ID")
	}
}

func TestEmbeddedCuesheet(t *testing.T) {
	ff := setup(t)
	ff.tags["img.flac"] = map[string]string{"CUESHEET": twoTracks, "ALBUM": "Tag Album"}
	ff.tags["bad.flac"] = map[string]string{"CUESHEET": "garbage", "TITLE": "Plain"}
	root := t.TempDir()
	touch(t, root, map[string]string{"img.flac": "x", "bad.flac": "x"})
	Scan([]string{root})
	if got := names(Children(root)); got != "file:bad.flac cue:img.flac" {
		t.Fatal(got)
	}
	if got := names(Children(filepath.Join(root, "img.flac"))); got != "track:One track:Two" {
		t.Fatal(got)
	}
	if got := TracksUnder(filepath.Join(root, "bad.flac")); len(got) != 1 || got[0].Title != "Plain" {
		t.Fatalf("%+v", got)
	}
}

func TestBrokenFilesAreSkipped(t *testing.T) {
	ff := setup(t)
	ff.bad["broken.flac"] = true
	root := t.TempDir()
	touch(t, root, map[string]string{
		"broken.flac": "x", "unknown.flac": "x", "ok.flac": "x",
		"junk/junk.cue": "not a cue sheet", "orphan/o.cue": twoTracks,
		"ref/broken.cue": strings.Replace(twoTracks, "a.wav", "broken.flac", 1), "ref/broken.flac": "x",
	})
	Scan([]string{root})
	if got := names(Children(root)); got != "file:ok.flac" {
		t.Fatal(got)
	}
}

func TestTrackIDsAreStable(t *testing.T) {
	setup(t)
	root := t.TempDir()
	touch(t, root, map[string]string{"a.flac": "x", "a.cue": twoTracks})
	Scan([]string{root})
	first := TracksUnder(root)[1].ID
	Scan([]string{root})
	if TracksUnder(root)[1].ID != first || len(first) != 16 {
		t.Fatalf("%s", first)
	}
}

func TestRescanUsesCacheUntilFileChanges(t *testing.T) {
	ff := setup(t)
	root := t.TempDir()
	touch(t, root, map[string]string{"a.flac": "x"})
	Scan([]string{root})
	Scan([]string{root})
	if ff.reads != 1 {
		t.Fatalf("%d header reads, want 1", ff.reads)
	}
	touch(t, root, map[string]string{"a.flac": "longer"})
	Scan([]string{root})
	if ff.reads != 2 {
		t.Fatalf("%d header reads, want 2 after a change", ff.reads)
	}
	if fileMeta(filepath.Join(root, "absent.flac")) != nil {
		t.Fatal("meta of a missing file")
	}
}

func writeCache(t *testing.T, p string, m map[string]*FileMeta) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Dir(p), map[string]string{filepath.Base(p): string(data)})
}

func TestCacheSurvivesRestartAndLegacyName(t *testing.T) {
	setup(t)
	writeCache(t, legacyCachePath(), map[string]*FileMeta{"old": {Size: 7}})
	loadMetaCache()
	if metaCache["old"] == nil {
		t.Fatal("the cache from before the rename was not read")
	}
	saveMetaCache()
	metaCache, cacheLoaded = map[string]*FileMeta{}, false
	_ = os.Remove(legacyCachePath())
	loadMetaCache()
	if metaCache["old"] == nil || metaCache["old"].Size != 7 {
		t.Fatalf("%+v", metaCache)
	}
	metaCache["new"] = &FileMeta{}
	loadMetaCache() // read once per process
	if metaCache["new"] == nil {
		t.Fatal("the cache was reloaded over live entries")
	}
}

func TestCorruptCacheIsIgnored(t *testing.T) {
	setup(t)
	touch(t, filepath.Dir(cachePath()), map[string]string{filepath.Base(cachePath()): "{broken"})
	loadMetaCache()
	if len(metaCache) != 0 {
		t.Fatalf("%+v", metaCache)
	}
}

func TestRealFLACMeta(t *testing.T) {
	reset()
	path := filepath.Join(t.TempDir(), "real.flac")
	n := testkit.WriteFLAC(t, path, testkit.FLAC{
		Frames: 3, BlockSize: 4096, LastBlock: 100,
		Tags:    [][2]string{{"title", "Lower"}, {"TITLE", "Second value is ignored"}},
		Picture: []byte("cover"),
	})
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := readFlacMeta(path, st)
	if err != nil {
		t.Fatal(err)
	}
	if m.SampleRate != 44100 || m.Channels != 2 || m.Bits != 16 || !m.HasPicture || m.Tags["TITLE"] != "Lower" || m.Size != st.Size() {
		t.Fatalf("%+v", m)
	}
	if m.NSamples != n {
		t.Fatalf("%d samples, want %d", m.NSamples, n)
	}
	data, mime, err := readEmbeddedPicture(path)
	if err != nil || string(data) != "cover" || mime != "image/jpeg" {
		t.Fatalf("%q %q %v", data, mime, err)
	}
	if _, err := readFlacMeta(path+".absent", st); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, _, err := readEmbeddedPicture(path + ".absent"); err == nil {
		t.Fatal("missing file accepted")
	}
	plain := filepath.Join(t.TempDir(), "plain.flac")
	testkit.WriteFLAC(t, plain, testkit.FLAC{Frames: 1, BlockSize: 4096})
	if _, _, err := readEmbeddedPicture(plain); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%v", err)
	}
}

func TestFindArt(t *testing.T) {
	reset()
	root := t.TempDir()
	touch(t, root, map[string]string{
		"folderart/Cover.JPG": "x", "folderart/other.png": "x", "folderart/t.cue": "x",
		"scans/Covers/album cover big.webp": "x", "scans/t.cue": "x",
		"front/my front.png": "x", "front/t.cue": "x",
		"parent/folder.jpeg": "x", "parent/CD1/t.cue": "x",
		"none/readme.txt": "x", "none/t.cue": "x",
		"nocover/picture.gif": "x", "nocover/t.cue": "x",
	})
	orig := readEmbeddedPicture
	t.Cleanup(func() { readEmbeddedPicture = orig })
	readEmbeddedPicture = func(string) ([]byte, string, error) { return nil, "", os.ErrNotExist }

	cases := []struct{ dir, want, mime string }{
		{"folderart", "Cover.JPG", "image/jpeg"},
		{"scans", "album cover big.webp", "image/webp"},
		{"front", "my front.png", "image/png"},
		{"parent/CD1", "folder.jpeg", "image/jpeg"},
	}
	for _, c := range cases {
		a := FindArt(&Track{Leaf: filepath.Join(root, filepath.FromSlash(c.dir), "t.cue")})
		if a == nil || filepath.Base(a.Path) != c.want || a.Mime != c.mime {
			t.Fatalf("%s: %+v", c.dir, a)
		}
	}
	if a := FindArt(&Track{Leaf: filepath.Join(root, "none", "t.cue")}); a != nil {
		t.Fatalf("%+v", a)
	}
	if a := FindArt(&Track{Leaf: filepath.Join(root, "nocover", "t.cue")}); a != nil {
		t.Fatalf("%+v", a)
	}

	readEmbeddedPicture = func(string) ([]byte, string, error) { return []byte("img"), "image/png", nil }
	a := FindArt(&Track{Leaf: filepath.Join(root, "none", "t.cue")})
	if a == nil || string(a.Data) != "img" || a.Mime != "image/png" {
		t.Fatalf("%+v", a)
	}
}
