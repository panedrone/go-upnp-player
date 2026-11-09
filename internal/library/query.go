package library

import (
	"path/filepath"
	"strings"
)

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
		// the node itself always plays; under a folder, a file covered by a cue
		// sheet is skipped — its cue tracks are already in the list
		if ll == lp || (strings.HasPrefix(ll, prefix) && !t.Covered) {
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
		if t.Covered {
			continue // found through its cue tracks
		}
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
