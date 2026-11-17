// Package library is the music library index: folder walking, cue, tags,
// and the tree for the UI. One instance per process, so it is package state.
package library

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/panedrone/go-upnp-player/internal/services"
)

// Track is a playable unit: a track from a cue or a whole file.
type Track struct {
	ID     string `json:"id"`
	Leaf   string `json:"leaf"` // tree node: path to the .cue or to the file
	Path   string `json:"path"` // audio file
	Number int    `json:"number"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Date   string `json:"date"`
	// Year: the album's year, from its folders (services.AlbumYear); the DATE tag of a
	// reissue is the reissue's
	Year string `json:"year,omitempty"`
	// Years: what an album header shows, the album's year and the release's after a slash
	// when they differ (services.AlbumYears)
	Years      string  `json:"years,omitempty"`
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
	// Covered: a whole-file track whose file a cue sheet in the same folder refers
	// to; folder playlists and search skip it in favour of the cue tracks.
	Covered bool `json:"-"`
	// DSD: a DSD file, played converted to PCM (ADR-028). SampleRate, Bits, Start and End
	// are then the PCM's; DSDRate is the file's own rate.
	DSD     *services.DSDFile `json:"-"`
	DSDRate uint32            `json:"dsdRate,omitempty"`
	// WAV: a WAV file, its samples read as they are
	WAV    *services.WAVFile `json:"-"`
	Format string            `json:"format"` // the file's format for the page: FLAC, WAV or DSD
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
	scans    int // completed scans: the UI reloads the tree when it changes
	scanMu   sync.Mutex

	metaCache   = map[string]*FileMeta{}
	metaCacheMu sync.Mutex

	// dsdSettings: how DSD becomes PCM; read by scans and by streams in the background
	dsdSettings atomic.Pointer[services.DSDSettings]
)

// SetDSD sets how DSD files are converted. The PCM rate is part of each DSD track, so a new
// rate takes a rescan; the gain applies from the next stream.
func SetDSD(s services.DSDSettings) {
	s = s.Normalize()
	dsdSettings.Store(&s)
}

// DSD is how DSD files are converted now.
func DSD() services.DSDSettings {
	if p := dsdSettings.Load(); p != nil {
		return *p
	}
	return services.DefaultDSD()
}

// Status is reported to the UI.
type Status struct {
	Scanning bool `json:"scanning"`
	Tracks   int  `json:"tracks"`
	// Scans counts completed scans. The UI polls once a second and a scan served
	// from the meta cache can finish in between, so Scanning alone may never be
	// seen as true; a changed count is what tells the page to reload the tree.
	Scans int `json:"scans"`
}

func GetStatus() Status {
	mu.RLock()
	defer mu.RUnlock()
	return Status{Scanning: scanning.Load(), Tracks: len(cur.tracks), Scans: scans}
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
		t.Year = services.AlbumYear(t.Leaf, t.Date)
		t.Years = services.AlbumYears(t.Year, t.Date)
	}
	saveMetaCache()

	mu.Lock()
	cur = idx
	scans++
	mu.Unlock()
}
