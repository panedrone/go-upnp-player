// Package player manages the queue and the selected renderer. One player per
// process: package state under a mutex, commands are serialized.
package player

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"sync"
	"time"

	"go-upnp-player/internal/config"
	"go-upnp-player/internal/library"
	"go-upnp-player/internal/services"
	"go-upnp-player/internal/stream"
	"go-upnp-player/internal/upnp"
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
		Error: lastErr, Discovering: discovering, Library: library.GetStatus(),
	}
	if index >= 0 && index < len(queue) {
		s.Track = library.Get(queue[index])
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
		if t := library.Get(id); t != nil {
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

// Discover finds renderers; if the selected one is absent, it takes the first one found.
func Discover() {
	mu.Lock()
	if discovering {
		mu.Unlock()
		return
	}
	discovering = true
	mu.Unlock()
	notify()

	c, cancel := ctx()
	defer cancel()
	found, err := upnp.Discover(c, 3*time.Second)

	mu.Lock()
	discovering = false
	if err == nil {
		renderers = found
		ok := false
		for _, d := range found {
			ok = ok || d.UDN == selected
		}
		if !ok && len(found) > 0 && selected == "" {
			selected = found[0].UDN
		}
	}
	mu.Unlock()
	setErr(err)
	refreshVolume()
}

// Select switches the renderer. If playing, the current track continues on the new one.
func Select(udn string) error {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	old, hadOld := device()
	mu.Lock()
	was := wantPlaying && !paused
	pos := position
	selected = udn
	nextURL = ""
	mu.Unlock()
	_ = config.Update(func(c *config.Config) { c.Renderer = udn })
	if hadOld && old.UDN != udn {
		c, cancel := ctx()
		_ = upnp.Pause(c, old) // not Stop, see Stop()
		cancel()
	}
	refreshVolume()
	if was {
		if err := startLocked(currentIndex(), pos); err != nil {
			setErr(err)
			return err
		}
	}
	notify()
	return nil
}

func currentIndex() int {
	mu.Lock()
	defer mu.Unlock()
	return index
}

// PlayQueue sets a new queue and starts at position i.
func PlayQueue(ids []string, i int) error {
	if i < 0 || i >= len(ids) {
		return fmt.Errorf("index %d out of range", i)
	}
	cmdMu.Lock()
	defer cmdMu.Unlock()
	mu.Lock()
	queue = append([]string(nil), ids...)
	queueVersion++
	mu.Unlock()
	err := startLocked(i, 0)
	savePlaylist()
	setErr(err)
	return err
}

// SetQueue builds the playlist without touching playback. If the playing track
// is in the new playlist, playback continues through the new one from it; if not, it
// plays out and the first track of the new playlist comes next.
func SetQueue(ids []string) {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	mu.Lock()
	cur := ""
	if index >= 0 && index < len(queue) {
		cur = queue[index]
	}
	queue = append([]string(nil), ids...)
	queueVersion++
	index = -1
	for i, id := range queue {
		if id == cur {
			index = i
			break
		}
	}
	playingNow := wantPlaying
	nextURL = "" // a previously queued next belongs to the old playlist
	mu.Unlock()
	savePlaylist()
	if playingNow {
		if d, ok := device(); ok {
			queueNext(d)
		}
	}
	notify()
}

// PlayIndex jumps to a track in the current queue.
func PlayIndex(i int) error {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	err := startLocked(i, 0)
	setErr(err)
	return err
}

// startLocked starts track i of the queue; call it under cmdMu.
func startLocked(i int, seek float64) error {
	d, ok := device()
	if !ok {
		return fmt.Errorf("no renderer selected")
	}
	mu.Lock()
	if i < 0 || i >= len(queue) {
		mu.Unlock()
		return fmt.Errorf("index %d out of range", i)
	}
	id := queue[i]
	mu.Unlock()
	t := library.Get(id)
	if t == nil {
		return fmt.Errorf("track %s is not in the library anymore", id)
	}
	uri, meta := media(d, t)
	c, cancel := ctx()
	defer cancel()
	// No Stop before changing the URI: on the FiiO SR11 (gmrender), Stop during
	// playback hangs control dead until a reboot (2026-11-09).
	// gmrender handles SetAVTransportURI during playback on its own.
	if err := upnp.SetURI(c, d, uri, meta); err != nil {
		return err
	}
	if err := upnp.Play(c, d); err != nil {
		return err
	}
	if seek > 1 {
		waitPlaying(d)
		_ = upnp.Seek(c, d, seek)
	}
	mu.Lock()
	index, wantPlaying, paused = i, true, false
	lastCmd = time.Now()
	nextURL = ""
	position, duration = seek, t.Duration
	mu.Unlock()
	savePlaylist()
	queueNext(d)
	notify()
	return nil
}

func waitPlaying(d upnp.Device) {
	for range 20 {
		c, cancel := ctx()
		ti, err := upnp.GetTransportInfo(c, d)
		cancel()
		if err == nil && ti.State == "PLAYING" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// queueNext queues the next track via SetNextAVTransportURI, which gives gapless playback.
// If unsupported, no harm done: DecideTick starts the next track itself.
func queueNext(d upnp.Device) {
	mu.Lock()
	ni := index + 1
	if ni >= len(queue) {
		mu.Unlock()
		return
	}
	id := queue[ni]
	mu.Unlock()
	t := library.Get(id)
	if t == nil {
		return
	}
	uri, meta := media(d, t)
	c, cancel := ctx()
	defer cancel()
	if err := upnp.SetNextURI(c, d, uri, meta); err != nil {
		return
	}
	mu.Lock()
	nextURL = uri
	mu.Unlock()
}

// media builds the track link and DIDL metadata for a specific renderer.
func media(d upnp.Device, t *library.Track) (uri, meta string) {
	var mime, ext string
	var size int64
	if t.FromCue || stream.EQActive() {
		mime = services.PickMime(d.SinkProtocols, "audio/wav", "audio/x-wav")
		ext = "wav"
		size = services.NewWavLayout(t.SampleRate, t.Channels, t.Bits, t.End-t.Start).TotalLen()
	} else {
		mime = services.PickMime(d.SinkProtocols, "audio/flac", "audio/x-flac")
		ext = "flac"
		size = t.FileSize
	}
	base := "http://" + d.LocalIP + ":" + strconv.Itoa(mediaPort)
	uri = base + "/media/track/" + t.ID + "." + ext + "?ct=" + url.QueryEscape(mime)
	meta = services.BuildDIDL(services.TrackMeta{
		Title: t.Title, Artist: t.Artist, Album: t.Album, Date: t.Date, Genre: t.Genre,
		TrackNumber: t.Number, Duration: t.Duration, SampleRate: t.SampleRate,
		Bits: t.Bits, Channels: t.Channels, Size: size, Mime: mime, URL: uri,
		ArtURL: base + "/media/art/" + t.ID,
	})
	return uri, meta
}

// Pause / Resume / Stop / Next / Prev / Seek / SetVolume are UI commands.

func Pause() error {
	return do(func(c context.Context, d upnp.Device) error {
		if err := upnp.Pause(c, d); err != nil {
			return err
		}
		mu.Lock()
		paused = true
		mu.Unlock()
		return nil
	})
}

func Resume() error {
	mu.Lock()
	idle := !wantPlaying
	i := max(index, 0) // playlist built but not yet played: start from the first track
	n := len(queue)
	mu.Unlock()
	if idle {
		if n == 0 {
			return fmt.Errorf("playlist is empty")
		}
		return PlayIndex(i)
	}
	return do(func(c context.Context, d upnp.Device) error {
		if err := upnp.Play(c, d); err != nil {
			return err
		}
		mu.Lock()
		paused = false
		lastCmd = time.Now()
		mu.Unlock()
		return nil
	})
}

// Stop, for the user, is Pause on the renderer plus clearing the intent to play:
// on the FiiO SR11 (gmrender), AVTransport.Stop during playback brings down
// the whole UPnP service until a power cycle (seen live 2026-11-09).
// The next Play restarts the current track from the beginning.
func Stop() error {
	return do(func(c context.Context, d upnp.Device) error {
		mu.Lock()
		wantPlaying, paused = false, false
		nextURL = ""
		mu.Unlock()
		return upnp.Pause(c, d)
	})
}

func Next() error {
	mu.Lock()
	i := index + 1
	n := len(queue)
	mu.Unlock()
	if i >= n {
		return nil
	}
	return PlayIndex(i)
}

func Prev() error {
	mu.Lock()
	i := index - 1
	pos := position
	mu.Unlock()
	if pos > 3 || i < 0 {
		return Seek(0)
	}
	return PlayIndex(i)
}

func Seek(sec float64) error {
	return do(func(c context.Context, d upnp.Device) error {
		if err := upnp.Seek(c, d, sec); err != nil {
			return err
		}
		mu.Lock()
		position = sec
		lastCmd = time.Now()
		mu.Unlock()
		return nil
	})
}

func SetVolume(v int) error {
	v = max(0, min(100, v))
	return do(func(c context.Context, d upnp.Device) error {
		if err := upnp.SetVolume(c, d, v); err != nil {
			return err
		}
		mu.Lock()
		volume = v
		mu.Unlock()
		return nil
	})
}

func do(fn func(context.Context, upnp.Device) error) error {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	d, ok := device()
	if !ok {
		return fmt.Errorf("no renderer selected")
	}
	c, cancel := ctx()
	defer cancel()
	err := fn(c, d)
	setErr(err)
	return err
}

func refreshVolume() {
	d, ok := device()
	if !ok {
		return
	}
	c, cancel := ctx()
	defer cancel()
	v, err := upnp.GetVolume(c, d)
	if err != nil {
		return
	}
	mu.Lock()
	volume = v
	mu.Unlock()
	notify()
}

// Tick is one renderer poll: position, state, queue advance.
// Called by the worker once a second.
func Tick() {
	if !cmdMu.TryLock() {
		return // a command is in progress; poll next time
	}
	defer cmdMu.Unlock()
	d, ok := device()
	if !ok {
		return
	}
	c, cancel := ctx()
	defer cancel()
	ti, err := upnp.GetTransportInfo(c, d)
	if err != nil {
		mu.Lock()
		transport = "OFFLINE"
		mu.Unlock()
		notify()
		return
	}
	pi, _ := upnp.GetPositionInfo(c, d)

	mu.Lock()
	transport = ti.State
	if pi.Duration > 0 {
		duration = pi.Duration
	}
	lastPos := position
	position = pi.Position
	in := services.TickInput{
		WantPlaying:  wantPlaying && !paused,
		State:        ti.State,
		SinceCmd:     time.Since(lastCmd),
		TrackURI:     pi.TrackURI,
		NextURL:      nextURL,
		HasNext:      index+1 < len(queue),
		Duration:     pi.Duration,
		Position:     pi.Position,
		LastPosition: lastPos,
		CurDuration:  trackDuration(index),
		NextDuration: trackDuration(index + 1),
	}
	tickCount++
	poll := tickCount%10 == 0
	mu.Unlock()

	switch services.DecideTick(in) {
	case services.TickAdvanced:
		mu.Lock()
		index++
		nextURL = ""
		lastCmd = time.Now()
		mu.Unlock()
		savePlaylist()
		queueNext(d)
	case services.TickPlayNext:
		setErr(startLocked(currentIndex()+1, 0))
	case services.TickFinished:
		mu.Lock()
		wantPlaying = false
		mu.Unlock()
	}
	if poll {
		if v, err := upnp.GetVolume(c, d); err == nil {
			mu.Lock()
			volume = v
			mu.Unlock()
		}
	}
	notify()
}

// SetEQ applies new equalizer settings. The renderer has already buffered several
// seconds of audio with the old settings, so while playing we re-request the stream
// from the current position: by seeking (same URI) or by restarting the track if
// the stream type changed (FLAC <-> WAV: the equalizer was turned on/off).
func SetEQ(s services.EQSettings) error {
	s = s.Normalize()
	before := stream.EQActive()
	stream.SetEQ(s)
	if err := config.Update(func(c *config.Config) { c.EQ = s }); err != nil {
		return err
	}
	mu.Lock()
	active := wantPlaying && !paused && index >= 0
	pos := position
	i := index
	mu.Unlock()
	if !active {
		return nil
	}
	t := library.Get(currentID())
	if t == nil {
		return nil
	}
	if !t.FromCue && before != stream.EQActive() {
		cmdMu.Lock()
		defer cmdMu.Unlock()
		err := startLocked(i, pos)
		setErr(err)
		return err
	}
	if !t.FromCue && !stream.EQActive() {
		return nil // raw FLAC, the equalizer does not apply to it
	}
	return Seek(pos)
}

// trackDuration returns the length of a queue track from the library; call it under mu.
func trackDuration(i int) float64 {
	if i < 0 || i >= len(queue) {
		return 0
	}
	if t := library.Get(queue[i]); t != nil {
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

// Shutdown runs before the process exits: if playing, it pauses the renderer
// (not Stop, see Stop()). Otherwise it plays out its buffer and goes silent
// mid-phrase, and the next player finds it in a strange state.
func Shutdown() {
	mu.Lock()
	playing := wantPlaying && !paused
	mu.Unlock()
	if playing {
		_ = Stop()
	}
}
