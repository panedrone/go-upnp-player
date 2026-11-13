package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/panedrone/go-upnp-player/cmd/go-upnp-player/window"
)

// BuildID identifies this exe: a short hash of the executable file, so every
// build differs and copies of the same build match.
func BuildID() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// RunningBuild reports whether the port is answered by go-upnp-player, and its build
// and version ("" for builds before /api/version existed).
func RunningBuild(ui string) (running bool, build, version string) {
	if !alreadyRunning(ui) {
		return false, "", ""
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(ui + "api/version")
	if err != nil {
		return true, "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	var v struct {
		Build   string `json:"build"`
		Version string `json:"version"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&v) != nil {
		return true, "", ""
	}
	return true, v.Build, v.Version
}

// TakeOver asks the running player to quit (the same graceful path as the Quit
// button: renderer paused first) and binds the port once it is free.
func TakeOver(ui, addr string) (net.Listener, error) {
	c := http.Client{Timeout: 3 * time.Second}
	if resp, err := c.Post(ui+"api/shutdown", "application/json", nil); err == nil {
		_ = resp.Body.Close()
	}
	var lastErr error
	for range 40 { // up to ~10 s: its shutdown waits up to 3 s for open streams
		ln, err := net.Listen("tcp4", addr)
		if err == nil {
			return ln, nil
		}
		lastErr = err
		time.Sleep(250 * time.Millisecond)
	}
	return nil, lastErr
}

// ShowRunningWindow asks the running player to bring its window to the front; false when
// it has none (it runs in the browser) or is a build without windows.
func ShowRunningWindow(ui string) bool {
	window.AllowForeground()
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Post(ui+"api/window/show", "application/json", nil)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

// alreadyRunning reports whether the port is answered by go-upnp-player itself (its /api/state).
func alreadyRunning(ui string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(ui + "api/state")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	var st struct {
		Renderers json.RawMessage `json:"renderers"`
		Library   json.RawMessage `json:"library"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&st) != nil {
		return false
	}
	return st.Library != nil
}
