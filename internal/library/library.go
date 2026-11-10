// Package library is the music library index: folder walking, cue, tags,
// and the tree for the UI. One instance per process, so it is package state.
package library

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	// Covered: a whole-file track whose file a cue sheet in the same folder refers
	// to; folder playlists and search skip it in favour of the cue tracks.
	Covered bool `json:"-"`
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
)

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
	}
	saveMetaCache()

	mu.Lock()
	cur = idx
	scans++
	mu.Unlock()
}
