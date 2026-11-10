// Package config holds settings that survive a restart: library folders
// and the last selected renderer. The file lives in the user config directory.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/panedrone/go-upnp-player/internal/services"
)

type Config struct {
	Roots    []string            `json:"roots"`
	Renderer string              `json:"renderer"` // UDN
	EQ       services.EQSettings `json:"eq"`
	Playlist Playlist            `json:"playlist"`
}

// Playlist is the playlist and the current track (library track IDs; an ID is stable
// as long as the file/cue path and the track layout do not change).
type Playlist struct {
	IDs   []string `json:"ids"`
	Index int      `json:"index"`
	// Source: the library tree node (folder, cue or file) the playlist was built
	// from; empty for a playlist built from search results
	Source string `json:"source,omitempty"`
	// Position: where Play resumes the current track after a restart (the player was
	// closed or updated while playing or paused); 0 starts it over
	Position float64 `json:"position,omitempty"`
}

var (
	mu  sync.Mutex
	cur Config
)

// appDir is the folder name under the user config/cache directories.
// legacyAppDir is the name used before the project was renamed (2026-11-09);
// files are still read from there once, so settings are not lost.
const (
	appDir       = "go-upnp-player"
	legacyAppDir = "upnp-player"
)

func path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return appDir + ".json"
	}
	return filepath.Join(dir, appDir, "config.json")
}

func legacyPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, legacyAppDir, "config.json")
}

// Load reads the file; a missing file is not an error but an empty config.
// If only the pre-rename file exists, it is read instead; the next Update
// writes the new location.
func Load() (Config, error) {
	mu.Lock()
	defer mu.Unlock()
	data, err := os.ReadFile(path())
	if os.IsNotExist(err) && legacyPath() != "" {
		data, err = os.ReadFile(legacyPath())
	}
	if os.IsNotExist(err) {
		return cur, nil
	}
	if err != nil {
		return cur, err
	}
	if err := json.Unmarshal(data, &cur); err != nil {
		return cur, err
	}
	return cur, nil
}

// Get returns the current settings.
func Get() Config {
	mu.Lock()
	defer mu.Unlock()
	c := cur
	c.Roots = append([]string(nil), cur.Roots...)
	return c
}

// Update changes the settings and saves the file.
func Update(fn func(*Config)) error {
	mu.Lock()
	defer mu.Unlock()
	fn(&cur)
	p := path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
