package player

import (
	"context"
	"fmt"
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
	"github.com/panedrone/go-upnp-player/internal/upnp"
	"time"
)

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
	idle := !wantPlaying || stalled // a hung renderer ignores Play: go through a real start, which reports the failure
	i := max(index, 0)              // playlist built but not yet played: start from the first track
	n := len(queue)
	at := resumeAt
	mu.Unlock()
	if at >= 0 && n > 0 {
		// paused, then the renderer was switched: start the track on the new one
		// at the position it was paused at
		cmdMu.Lock()
		defer cmdMu.Unlock()
		err := startLocked(i, at)
		setErr(err)
		return err
	}
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
		resumeAt = -1 // Stop means "start the track over" next time
		nextURL = ""
		mu.Unlock()
		return halt(c, d)
	})
}

// halt takes a renderer out of play: a real Stop where that is safe, Pause otherwise
// (ADR-002). A stopped gmrender drops its pipeline and HTTP connection; a paused one
// keeps both, idle, which on the BeagleBone was part of the racing trigger (ADR-007).
func halt(c context.Context, d upnp.Device) error {
	if services.StopSafe(d.Model) {
		return upnp.Stop(c, d)
	}
	return upnp.Pause(c, d)
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
	t := lookupTrack(currentID())
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

// Shutdown runs before the process exits: if playing, it pauses the renderer
// (not Stop, see Stop()). Otherwise it plays out its buffer and goes silent
// mid-phrase, and the next player finds it in a strange state.
func Shutdown() {
	mu.Lock()
	playing := wantPlaying && !paused
	busy, pos := wantPlaying, position
	mu.Unlock()
	if playing {
		_ = Stop()
	}
	if busy && pos > 1 {
		// an update or a restart is not a Stop: the next Play continues the track
		// where it was (it used to start the track over, 2026-11-10)
		mu.Lock()
		resumeAt = pos
		mu.Unlock()
		savePlaylist()
	}
}
