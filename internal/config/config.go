// Package config holds settings that survive a restart: library folders
// and the last selected renderer. The file lives in the user config directory.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"upnp-player/internal/services"
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
}

var (
	mu  sync.Mutex
	cur Config
)

func path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "upnp-player.json"
	}
	return filepath.Join(dir, "upnp-player", "config.json")
}

// Load reads the file; a missing file is not an error but an empty config.
func Load() (Config, error) {
	mu.Lock()
	defer mu.Unlock()
	data, err := os.ReadFile(path())
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
