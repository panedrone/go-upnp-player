// Package player manages the queue and the selected renderer. One player per
// process: package state under a mutex, commands are serialized.
package player

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/upnp"
)

// State is a snapshot for the UI (SSE and /api/state).
type State struct {
	Renderers    []upnp.Device  `json:"renderers"`
	Selected     string         `json:"selected"`
	Transport    string         `json:"transport"` // PLAYING/PAUSED_PLAYBACK/STOPPED/...
	WantPlaying  bool           `json:"wantPlaying"`
	Paused       bool           `json:"paused"`
	Position     float64        `json:"position"`
	Duration     float64        `json:"duration"`
	Volume       int            `json:"volume"`
	Index        int            `json:"index"`
	QueueVersion int            `json:"queueVersion"`
	QueueLen     int            `json:"queueLen"`
	Track        *library.Track `json:"track"`
	Error        string         `json:"error,omitempty"`
	Discovering  bool           `json:"discovering"`
	Library      library.Status `json:"library"`
	// Build of the running exe: the UI reloads itself when it changes (after an update)
	Build string `json:"build"`
}

var (
	cmdMu sync.Mutex // one renderer command at a time
	mu    sync.Mutex // guards the state below

	renderers    []upnp.Device
	selected     string
	queue        []string
	queueVersion int
	index        = -1
	wantPlaying  bool
	paused       bool
	lastCmd      time.Time
	nextURL      string
	transport    string
	position     float64
	duration     float64
	volume       = -1
	lastErr      string
	discovering  bool
	mediaPort    int
	tickCount    int
	// resumeAt: the renderer was switched while paused; the new one has not got
	// the track yet, so the next Play starts it there at this position (-1: none)
	resumeAt = -1.0

	// Build identifies the running exe; set once in main before serving.
	Build string

	subsMu sync.Mutex
	subs   = map[chan struct{}]struct{}{}
)

// Init sets the port on which Fiber serves /media (it goes into the links).
func Init(port int) {
	cfg := config.Get()
	mediaPort = port
	selected = cfg.Renderer
	// playlist from the previous run; do not start playing, Play resumes from the same track
	queue = append([]string(nil), cfg.Playlist.IDs...)
	index = -1
	if cfg.Playlist.Index >= 0 && cfg.Playlist.Index < len(queue) {
		index = cfg.Playlist.Index
	}
	queueVersion++
}

// savePlaylist writes the playlist and the current track to the config so they survive a restart.
func savePlaylist() {
	mu.Lock()
	p := config.Playlist{IDs: append([]string(nil), queue...), Index: index}
	mu.Unlock()
	if err := config.Update(func(c *config.Config) { c.Playlist = p }); err != nil {
		log.Printf("player: save playlist: %v", err)
	}
}

// Subscribe returns a channel of state change notifications (for SSE).
func Subscribe() (ch chan struct{}, cancel func()) {
	ch = make(chan struct{}, 1)
	subsMu.Lock()
	subs[ch] = struct{}{}
	subsMu.Unlock()
	return ch, func() {
		subsMu.Lock()
		delete(subs, ch)
		subsMu.Unlock()
	}
}

func notify() {
	subsMu.Lock()
	defer subsMu.Unlock()
	for ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Snapshot returns the current state.
func Snapshot() State {
	mu.Lock()
	defer mu.Unlock()
	s := State{
		Renderers: renderers, Selected: selected, Transport: transport,
		WantPlaying: wantPlaying, Paused: paused, Position: position, Duration: duration,
		Volume: volume, Index: index, QueueVersion: queueVersion, QueueLen: len(queue),
		Error: lastErr, Discovering: discovering, Library: library.GetStatus(), Build: Build,
	}
	if index >= 0 && index < len(queue) {
		s.Track = lookupTrack(queue[index])
	}
	return s
}

// Queue returns the tracks of the current queue.
func Queue() (int, []*library.Track) {
	mu.Lock()
	ids := append([]string(nil), queue...)
	v := queueVersion
	mu.Unlock()
	out := make([]*library.Track, 0, len(ids))
	for _, id := range ids {
		if t := lookupTrack(id); t != nil {
			out = append(out, t)
		}
	}
	return v, out
}

func setErr(err error) {
	mu.Lock()
	if err != nil {
		lastErr = err.Error()
		log.Printf("player: %v", err)
	} else {
		lastErr = ""
	}
	mu.Unlock()
	notify()
}

func device() (upnp.Device, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, d := range renderers {
		if d.UDN == selected {
			return d, true
		}
	}
	return upnp.Device{}, false
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 6*time.Second)
}

func currentIndex() int {
	mu.Lock()
	defer mu.Unlock()
	return index
}

// trackDuration returns the length of a queue track from the library; call it under mu.
func trackDuration(i int) float64 {
	if i < 0 || i >= len(queue) {
		return 0
	}
	if t := lookupTrack(queue[i]); t != nil {
		return t.Duration
	}
	return 0
}

func currentID() string {
	mu.Lock()
	defer mu.Unlock()
	if index < 0 || index >= len(queue) {
		return ""
	}
	return queue[index]
}
