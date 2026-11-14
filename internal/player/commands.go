package player

import (
	"context"
	"fmt"
	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/stream"
	"github.com/panedrone/go-upnp-player/internal/upnp"
	"log"
	"time"
)

// Pause / Resume / Stop / Next / Prev / Seek / SetVolume are UI commands.

func Pause() error {
	mu.Lock()
	if !pendingSince.IsZero() {
		// a refused start is being retried: pausing ends that, the position stays
		pendingSince, paused = time.Time{}, true
		if pendingPos > 1 {
			resumeAt = pendingPos
		}
		mu.Unlock()
		return nil
	}
	mu.Unlock()
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

// Stop is the Stop button: AVTransport.Stop on every renderer alike, and the intent to
// play cleared; the next Play starts the current track over (ADR-025, the owner's call,
// 2026-11-14). The jumps between tracks are not this: see jumpLocked.
func Stop() error {
	return do(stopLocked)
}

// stopLocked is Stop under cmdMu. Every step waits for the renderer to report what it
// leads to (confirm).
func stopLocked(c context.Context, d upnp.Device) error {
	mu.Lock()
	wantPlaying, paused = false, false
	pendingSince = time.Time{} // also ends the retries of a refused start
	resumeAt = -1              // Stop means "start the track over" next time
	nextURL, nextRun = "", runPlan{}
	position = 0 // and the slider says so: the next Play starts the track over
	mu.Unlock()
	return stopRenderer(c, d)
}

// stopRenderer asks the renderer its state first: already STOPPED (or holding nothing),
// it gets no command. Otherwise one Stop, never repeated; then only questions, until it
// reports STOPPED.
func stopRenderer(c context.Context, d upnp.Device) error {
	if ti, err := getTransportInfo(c, d); err == nil && (ti.State == "STOPPED" || ti.State == "NO_MEDIA_PRESENT") {
		return nil
	}
	if err := upnp.Stop(c, d); err != nil {
		return err
	}
	if !confirm(d, func(st renderState) bool { return st.state == "STOPPED" }) {
		log.Printf("player: %s did not report itself stopped", d.Name)
	}
	return nil
}

// quiet takes a renderer out of play without closing its sound output: paused, then its
// stream let go to silence and the renderer seeked where it stands (at, in the stream cur),
// so that the moment of music a paused renderer holds is silence; its next Play let that
// moment be heard (the FiiO, 2026-11-10). Each step confirmed by the renderer's report.
// It is the first half of every jump (ADR-022).
func quiet(c context.Context, d upnp.Device, cur string, at float64) error {
	if err := upnp.Pause(c, d); err != nil {
		return err
	}
	if !confirm(d, func(st renderState) bool {
		return (st.state == "PAUSED_PLAYBACK" || st.state == "STOPPED") && st.still
	}) {
		log.Printf("player: %s did not report itself paused", d.Name)
	}
	if id, ok := runIDOf(cur); ok {
		stream.RetireRun(id)
		sc, cancel := ctx() // the confirmation above may have outlasted c
		defer cancel()
		sent := time.Now()
		if seekRenderer(sc, d, at) == nil && !confirmAt(d, cur, at, sent) {
			log.Printf("player: %s did not report the position %.0f s after a pause", d.Name, at)
		}
	}
	return nil
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

// Prev goes to the previous track wherever the current one is, as foobar2000 does
// (it used to restart the current track past its first 3 s; owner, 2026-11-11).
// On the first track it goes back to its start.
func Prev() error {
	i := currentIndex() - 1
	if i < 0 {
		return Seek(0)
	}
	return PlayIndex(i)
}

func Seek(sec float64) error {
	mu.Lock()
	i := index
	at, inStream := streamTimeLocked(i, sec)
	if len(runIDs) == 0 {
		at, inStream = sec, true // nothing started here (yet): the renderer's own stream
	}
	mu.Unlock()
	if !inStream {
		// the current stream does not hold that point (it starts later in the track,
		// ADR-015, or it is not the playlist's anymore): start the track again there
		cmdMu.Lock()
		defer cmdMu.Unlock()
		err := startLocked(i, sec)
		setErr(err)
		return err
	}
	mu.Lock()
	playing := wantPlaying && !paused
	mu.Unlock()
	if playing {
		// over playing music: the way a jump to a track goes, Stop and a start there
		// (jumpLocked); the FiiO stuttered through a plain seek, the slider's too
		cmdMu.Lock()
		defer cmdMu.Unlock()
		err := jumpLocked(i, sec)
		setErr(err)
		return err
	}
	return do(func(c context.Context, d upnp.Device) error {
		if err := seekRenderer(c, d, at); err != nil {
			return err
		}
		mu.Lock()
		position = sec
		seekSentLocked(at)
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
	stream.SetEQ(s)
	if err := config.Update(func(c *config.Config) { c.EQ = s }); err != nil {
		return err
	}
	mu.Lock()
	active := wantPlaying && !paused && index >= 0
	pos := position
	mu.Unlock()
	if !active {
		return nil
	}
	// every run is WAV decoded here (ADR-016): the equalizer applies to it, and a seek
	// to where it plays makes the renderer request the stream anew
	return Seek(pos)
}

// Shutdown runs before the process exits: if playing, it pauses the renderer
// (not Stop, see Stop()). Otherwise it plays out its buffer and goes silent
// mid-phrase, and the next player finds it in a strange state.
func Shutdown() {
	mu.Lock()
	playing := wantPlaying && !paused
	busy, pos := wantPlaying, position
	if services.AtEnd(pos, duration) {
		pos = 0 // the track has ended: the next Play starts it over, nothing to keep
	}
	mu.Unlock()
	if playing {
		_ = Stop()
	}
	if busy && pos > 1 {
		// an update or a restart is not a Stop: the next Play continues the track
		// where it was (it used to start the track over, 2026-11-10)
		mu.Lock()
		resumeAt = pos
		i := index
		mu.Unlock()
		savePlaylist()
		// one line per exit: the next process must pick this up (a lost position
		// after an update was reported 2026-11-10)
		log.Printf("player: shutdown: the next Play continues playlist track %d at %.0f s", i, pos)
	}
}
