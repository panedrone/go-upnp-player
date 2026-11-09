package player

import (
	"fmt"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
	"github.com/panedrone/go-upnp-player/internal/upnp"
	"net/url"
	"strconv"
	"time"
)

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
	t := lookupTrack(id)
	if t == nil {
		return fmt.Errorf("track %s is not in the library anymore", id)
	}
	uri, meta := media(d, t)
	c, cancel := ctx()
	defer cancel()

	// How to switch (2026-11-09, both renderers run gmrender):
	//   - gmrender takes a new SetAVTransportURI only from STOPPED; while playing
	//     or paused, Play keeps the old song going;
	//   - Stop is not an option: on the FiiO SR11 it hangs control until a power
	//     cycle, both during playback and from pause.
	// So a busy renderer is switched through its gapless path: the target goes
	// in as the "next" URI and the current track is seeked to its last second;
	// the renderer moves on by itself.
	ti, _ := upnp.GetTransportInfo(c, d)
	pi, _ := upnp.GetPositionInfo(c, d)
	plan := services.PlanSwitch(ti.State, pi.Duration)
	if plan.ViaNext && pi.TrackURI == uri && services.SameLength(pi.Duration, t.Duration) {
		// the renderer already has this very track: restart it in place. The URI
		// alone is not proof — gmrender reports the queued next URI early — so the
		// audible length must match too.
		if err := upnp.Seek(c, d, 0); err != nil {
			return err
		}
		if plan.Resume {
			if err := upnp.Play(c, d); err != nil {
				return err
			}
		}
		plan.ViaNext = false
	} else if plan.ViaNext {
		// Up to three attempts: if the renderer had already pulled a previously
		// queued "next" into its buffer, our SetNext comes too late and it lands
		// on that track instead (seen live 2026-11-09). The wrong track is then
		// the current one and the next attempt goes through.
		switched := false
		for attempt := 0; attempt < 3 && plan.ViaNext; attempt++ {
			if err := switchViaNext(d, plan, uri, meta); err != nil {
				return err
			}
			if waitSwitched(d, t.Duration) {
				switched = true
				break
			}
			c2, cancel2 := ctx()
			ti, _ = upnp.GetTransportInfo(c2, d)
			pi, _ = upnp.GetPositionInfo(c2, d)
			cancel2()
			plan = services.PlanSwitch(ti.State, pi.Duration)
		}
		if !switched && plan.ViaNext {
			// A hung gmrender accepts every command and moves nowhere; this used to be
			// reported as a successful start (BeagleBone, 2026-11-09).
			return fmt.Errorf("renderer %s did not switch to %q: it may be hung, restart it", d.Name, t.Title)
		}
		if !switched { // it stopped in the meantime, so a plain start works now
			if err := startDirect(d, uri, meta); err != nil {
				return err
			}
		}
	} else if err := startDirect(d, uri, meta); err != nil {
		return err
	}
	if seek > 1 {
		seekWhenReady(d, seek)
	}
	mu.Lock()
	resumeAt = -1
	index, wantPlaying, paused = i, true, false
	lastCmd = time.Now()
	nextURL = ""
	position, duration = seek, t.Duration
	frozenSince, stalled = time.Now(), false
	mu.Unlock()
	savePlaylist()
	queueNext(d)
	notify()
	return nil
}

// startDirect starts a track on a renderer that is not busy.
func startDirect(d upnp.Device, uri, meta string) error {
	c, cancel := ctx()
	defer cancel()
	if err := upnp.SetURI(c, d, uri, meta); err != nil {
		return err
	}
	return upnp.Play(c, d)
}

// waitSwitched waits (up to ~6 s) until the renderer reports the target track's
// length, i.e. the gapless switch has happened. Only then may the next "next"
// URI be queued, otherwise it would replace the target before the switch.
func waitSwitched(d upnp.Device, targetDur float64) bool {
	for range 30 {
		c, cancel := ctx()
		pi, err := upnp.GetPositionInfo(c, d)
		cancel()
		if err == nil && services.SwitchDone(pi.Duration, pi.Position, targetDur) {
			return true
		}
		sleep(200 * time.Millisecond)
	}
	return false
}

// switchViaNext: the target as the "next" URI, the current track seeked to its
// last second, Play if paused — the renderer moves on by itself.
func switchViaNext(d upnp.Device, plan services.SwitchPlan, uri, meta string) error {
	c, cancel := ctx()
	defer cancel()
	if err := upnp.SetNextURI(c, d, uri, meta); err != nil {
		return err
	}
	if err := upnp.Seek(c, d, plan.SeekTo); err != nil {
		return err
	}
	if plan.Resume {
		return upnp.Play(c, d)
	}
	return nil
}

// seekWhenReady seeks the freshly started track to pos and checks that it
// landed: gmrender silently ignores a Seek sent in the first moments after a
// stream starts, so a device switch used to restart the track from 0
// (2026-11-09). Retries for up to ~6 s.
func seekWhenReady(d upnp.Device, pos float64) {
	waitPlaying(d)
	for range 6 {
		c, cancel := ctx()
		_ = upnp.Seek(c, d, pos)
		cancel()
		sleep(700 * time.Millisecond)
		c, cancel = ctx()
		pi, err := upnp.GetPositionInfo(c, d)
		cancel()
		if err == nil && services.SeekLanded(pi.Position, pos) {
			return
		}
		sleep(300 * time.Millisecond)
	}
}

func waitPlaying(d upnp.Device) {
	for range 20 {
		c, cancel := ctx()
		ti, err := upnp.GetTransportInfo(c, d)
		cancel()
		if err == nil && ti.State == "PLAYING" {
			return
		}
		sleep(200 * time.Millisecond)
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
	t := lookupTrack(id)
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
