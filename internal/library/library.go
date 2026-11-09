// Package library is the music library index: folder walking, cue, tags,
// and the tree for the UI. One instance per process, so it is package state.
package library

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"go-upnp-player/internal/services"
)

// Track is a playable unit: a track from a cue or a whole file.
type Track struct {
	ID         string  `json:"id"`
	Leaf       string  `json:"leaf"` // tree node: path to the .cue or to the file
	Path       string  `json:"path"` // audio file
	Number     int     `json:"number"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album"`
	Date       string  `json:"date"`
	Genre      string  `json:"genre"`
	Start      uint64  `json:"-"`
	End        uint64  `json:"-"`
	SampleRate uint32  `json:"sampleRate"`
	Channels   uint8   `json:"channels"`
	Bits       uint8   `json:"bits"`
	Duration   float64 `json:"duration"`
	Seekable   bool    `json:"-"`
	FromCue    bool    `json:"fromCue"`
	FileSize   int64   `json:"-"`
	FileFrames uint64  `json:"-"` // full file length in samples
}

// Node is a tree element.
type Node struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Kind  string `json:"kind"` // dir | cue | file | track
	Count int    `json:"count"`
	// playable leaves (file, track) carry the track they play
	ID       string  `json:"id,omitempty"`
	Number   int     `json:"number,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

type index struct {
	tracks   []*Track
	byID     map[string]*Track
	children map[string][]Node // folder path -> visible children
	roots    []Node
}

var (
	mu       sync.RWMutex
	cur      = &index{byID: map[string]*Track{}, children: map[string][]Node{}}
	scanning atomic.Bool
	scanMu   sync.Mutex

	metaCache   = map[string]*FileMeta{}
	metaCacheMu sync.Mutex
)

// Status is reported to the UI.
type Status struct {
	Scanning bool `json:"scanning"`
	Tracks   int  `json:"tracks"`
}

func GetStatus() Status {
	mu.RLock()
	defer mu.RUnlock()
	return Status{Scanning: scanning.Load(), Tracks: len(cur.tracks)}
}

// Scan rebuilds the index. While a scan runs, readers see the old index.
func Scan(roots []string) {
	scanMu.Lock()
	defer scanMu.Unlock()
	scanning.Store(true)
	defer scanning.Store(false)

	loadMetaCache()
	idx := &index{byID: map[string]*Track{}, children: map[string][]Node{}}
	for _, root := range roots {
		root = filepath.Clean(root)
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			log.Printf("library: root %s is not a directory", root)
			continue
		}
		if scanDir(idx, root) > 0 {
			idx.roots = append(idx.roots, Node{Path: root, Name: root, Kind: "dir", Count: len(idx.children[root])})
		}
	}
	for _, t := range idx.tracks {
		idx.byID[t.ID] = t
	}
	saveMetaCache()

	mu.Lock()
	cur = idx
	mu.Unlock()
}

// scanDir walks a folder and returns the number of visible children.
func scanDir(idx *index, dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var cues, flacs []string
	var subdirs []string
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			subdirs = append(subdirs, full)
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".cue":
			cues = append(cues, full)
		case ".flac":
			flacs = append(flacs, full)
		}
	}

	var nodes []Node
	for _, sd := range subdirs {
		if n := scanDir(idx, sd); n > 0 {
			nodes = append(nodes, Node{Path: sd, Name: filepath.Base(sd), Kind: "dir", Count: n})
		}
	}

	covered := map[string]bool{}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	for _, cuePath := range cues {
		tracks := cueTracks(cuePath, flacs, exists, covered)
		if len(tracks) == 0 {
			continue
		}
		idx.tracks = append(idx.tracks, tracks...)
		nodes = append(nodes, Node{Path: cuePath, Name: filepath.Base(cuePath), Kind: "cue", Count: len(tracks)})
	}
	for _, f := range flacs {
		if covered[strings.ToLower(f)] {
			continue
		}
		tracks := fileTracks(f)
		if len(tracks) == 0 {
			continue
		}
		idx.tracks = append(idx.tracks, tracks...)
		kind := "file"
		if len(tracks) > 1 {
			kind = "cue" // embedded cuesheet
		}
		n := Node{Path: f, Name: filepath.Base(f), Kind: kind, Count: len(tracks)}
		if kind == "file" {
			n.ID, n.Number, n.Duration = tracks[0].ID, tracks[0].Number, tracks[0].Duration
		}
		nodes = append(nodes, n)
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		if (nodes[i].Kind == "dir") != (nodes[j].Kind == "dir") {
			return nodes[i].Kind == "dir"
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	if len(nodes) > 0 {
		idx.children[dir] = nodes
	}
	return len(nodes)
}

func cueTracks(cuePath string, flacs []string, exists func(string) bool, covered map[string]bool) []*Track {
	data, err := os.ReadFile(cuePath)
	if err != nil {
		return nil
	}
	sheet, err := services.ParseCue(data)
	if err != nil {
		log.Printf("library: %s: %v", cuePath, err)
		return nil
	}
	var out []*Track
	for _, cf := range sheet.Files {
		audio := services.ResolveCueFile(filepath.Dir(cuePath), cf.Name, exists, flacs)
		if audio == "" {
			continue
		}
		m := fileMeta(audio)
		if m == nil {
			continue
		}
		covered[strings.ToLower(audio)] = true
		for _, ti := range services.CueTracks(sheet, cf, m.SampleRate, m.NSamples, m.Tags) {
			out = append(out, newTrack(cuePath, audio, ti, m, true))
		}
	}
	return out
}

func fileTracks(path string) []*Track {
	m := fileMeta(path)
	if m == nil {
		return nil
	}
	// cue embedded in the CUESHEET tag (this is how foobar2000 writes it)
	if text := m.Tags["CUESHEET"]; text != "" {
		if sheet, err := services.ParseCue([]byte(text)); err == nil && len(sheet.Files) > 0 {
			var out []*Track
			for _, ti := range services.CueTracks(sheet, sheet.Files[0], m.SampleRate, m.NSamples, m.Tags) {
				out = append(out, newTrack(path, path, ti, m, true))
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []*Track{newTrack(path, path, services.TaggedTrack(path, m.NSamples, m.Tags), m, false)}
}

func newTrack(leaf, audio string, ti services.TrackInfo, m *FileMeta, fromCue bool) *Track {
	h := sha1.Sum([]byte(strings.ToLower(leaf) + "#" + strconv.Itoa(ti.Number) + "#" + strconv.FormatUint(ti.Start, 10)))
	dur := 0.0
	if m.SampleRate > 0 {
		dur = float64(ti.End-ti.Start) / float64(m.SampleRate)
	}
	return &Track{
		ID: hex.EncodeToString(h[:8]), Leaf: leaf, Path: audio, Number: ti.Number,
		Title: ti.Title, Artist: ti.Artist, Album: ti.Album, Date: ti.Date, Genre: ti.Genre,
		Start: ti.Start, End: ti.End, SampleRate: m.SampleRate, Channels: m.Channels, Bits: m.Bits,
		Duration: dur, Seekable: m.Seekable, FromCue: fromCue, FileSize: m.Size, FileFrames: m.NSamples,
	}
}

func fileMeta(path string) *FileMeta {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	key := strings.ToLower(path)
	metaCacheMu.Lock()
	c := metaCache[key]
	metaCacheMu.Unlock()
	if c != nil && c.ModTime == st.ModTime().UnixNano() && c.Size == st.Size() {
		return c
	}
	m, err := readFlacMeta(path, st)
	if err != nil {
		log.Printf("library: %v", err)
		return nil
	}
	if m.SampleRate == 0 || m.NSamples == 0 {
		log.Printf("library: %s: unknown length, skipped", path)
		return nil
	}
	metaCacheMu.Lock()
	metaCache[key] = m
	metaCacheMu.Unlock()
	return m
}

// --- on-disk metadata cache: a rescan does not reread unchanged files ---

func cachePath() string { return cacheFile("go-upnp-player") }

// legacyCachePath is the cache from before the project rename (2026-11-09);
// reading it once spares a full rescan of a large library.
func legacyCachePath() string { return cacheFile("upnp-player") }

func cacheFile(appDir string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, appDir, "meta-cache.json")
}

var cacheLoaded bool

func loadMetaCache() {
	if cacheLoaded {
		return
	}
	cacheLoaded = true
	p := cachePath()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		p = legacyCachePath()
		data, err = os.ReadFile(p)
	}
	if err != nil {
		return
	}
	m := map[string]*FileMeta{}
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("library: cache %s is corrupt, ignored: %v", p, err)
		return
	}
	metaCacheMu.Lock()
	metaCache = m
	metaCacheMu.Unlock()
}

func saveMetaCache() {
	p := cachePath()
	if p == "" {
		return
	}
	metaCacheMu.Lock()
	data, err := json.Marshal(metaCache)
	metaCacheMu.Unlock()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("library: save cache: %v", err)
		return
	}
	_ = os.Rename(tmp, p)
}

// --- index reads ---

// Children returns the children of a tree node; "" means the roots.
func Children(path string) []Node {
	mu.RLock()
	defer mu.RUnlock()
	if path == "" {
		return cur.roots
	}
	path = filepath.Clean(path)
	if nodes, ok := cur.children[path]; ok {
		return nodes
	}
	// a cue (or a FLAC with an embedded cue) expands into its tracks
	var out []Node
	for _, t := range cur.tracks {
		if t.Leaf == path && t.FromCue {
			out = append(out, Node{
				Path: path + "#" + t.ID, Name: t.Title, Kind: "track",
				ID: t.ID, Number: t.Number, Duration: t.Duration,
			})
		}
	}
	return out
}

// TracksUnder returns all tracks under a node (folder, cue or file) in tree order.
func TracksUnder(path string) []*Track {
	mu.RLock()
	defer mu.RUnlock()
	path = filepath.Clean(path)
	prefix := strings.ToLower(path) + string(filepath.Separator)
	lp := strings.ToLower(path)
	var out []*Track
	for _, t := range cur.tracks {
		ll := strings.ToLower(t.Leaf)
		if ll == lp || strings.HasPrefix(ll, prefix) {
			out = append(out, t)
		}
	}
	return out
}

// Search matches a case-insensitive substring against artist/album/title/path.
func Search(q string, limit int) []*Track {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	words := strings.Fields(q)
	mu.RLock()
	defer mu.RUnlock()
	var out []*Track
	for _, t := range cur.tracks {
		hay := strings.ToLower(t.Artist + " " + t.Album + " " + t.Title + " " + t.Leaf)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, t)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// Get returns a track by ID.
func Get(id string) *Track {
	mu.RLock()
	defer mu.RUnlock()
	return cur.byID[id]
}
