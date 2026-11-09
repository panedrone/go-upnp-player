package library

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Art is a track's cover: an image from the folder or one embedded in the FLAC.
type Art struct {
	Path string // image file, if one was found in the folder
	Data []byte // otherwise the embedded one
	Mime string
}

var (
	artCache   = map[string]string{} // folder -> found file ("" means none)
	artCacheMu sync.Mutex
)

var coverNames = []string{"folder", "cover", "front", "albumart", "albumartlarge"}
var coverDirs = []string{"", "covers", "cover", "scans", "artwork", "art"}

// FindArt looks up the cover for a track.
func FindArt(t *Track) *Art {
	dir := filepath.Dir(t.Leaf)
	if p := dirArt(dir); p != "" {
		return &Art{Path: p, Mime: mimeByExt(p)}
	}
	if data, mime, err := readEmbeddedPicture(t.Path); err == nil {
		return &Art{Data: data, Mime: mime}
	}
	// a disc in an album subfolder (CD1/CD2): the cover is in the parent
	if p := dirArt(filepath.Dir(dir)); p != "" {
		return &Art{Path: p, Mime: mimeByExt(p)}
	}
	return nil
}

func dirArt(dir string) string {
	key := strings.ToLower(dir)
	artCacheMu.Lock()
	if p, ok := artCache[key]; ok {
		artCacheMu.Unlock()
		return p
	}
	artCacheMu.Unlock()

	found := ""
	for _, sub := range coverDirs {
		found = imageIn(filepath.Join(dir, sub), sub != "")
		if found != "" {
			break
		}
	}
	artCacheMu.Lock()
	artCache[key] = found
	artCacheMu.Unlock()
	return found
}

// imageIn looks for an image with a "cover-like" name; in scan subfolders
// any image with front/cover in its name will also do.
func imageIn(dir string, loose bool) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var fallback string
	for _, e := range entries {
		if e.IsDir() || mimeByExt(e.Name()) == "" {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		for _, n := range coverNames {
			if stem == n {
				return filepath.Join(dir, e.Name())
			}
		}
		if fallback == "" && (strings.Contains(stem, "front") || (loose && strings.Contains(stem, "cover"))) {
			fallback = filepath.Join(dir, e.Name())
		}
	}
	return fallback
}

func mimeByExt(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	}
	return ""
}
