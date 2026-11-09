package library

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

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
