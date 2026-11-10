package player

import (
	"fmt"
	"time"

	"github.com/panedrone/go-upnp-player/internal/upnp"
)

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
	source = ""
	mu.Unlock()
	err := startLocked(i, 0)
	savePlaylist()
	setErr(err)
	return err
}

// SetQueue builds the playlist without touching playback. src is the tree node it
// was built from ("" for search results); the tree highlights it. If the playing track
// is in the new playlist, playback continues through the new one from it; if not, it
// plays out and the first track of the new playlist comes next.
func SetQueue(ids []string, src string) {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	mu.Lock()
	cur := ""
	if index >= 0 && index < len(queue) {
		cur = queue[index]
	}
	inRun := index - runQ // the current track's place in the stream the renderer plays
	queue = append([]string(nil), ids...)
	queueVersion++
	source = src
	index = -1
	for i, id := range queue {
		if id == cur {
			index = i
			break
		}
	}
	if index < 0 {
		resumeAt = -1 // the paused track left the playlist: Play starts the new one over
	} else {
		runQ = index - inRun // where the stream would start in the new playlist, if it still fits
	}
	playingNow := wantPlaying
	nextURL, nextRun = "", runPlan{} // a previously queued next belongs to the old playlist
	mu.Unlock()
	savePlaylist()
	if playingNow {
		if d, ok := device(); ok {
			queueNext(d)
		}
	}
}

// PlayIndex jumps to a track in the current queue.
// A track of the stream the renderer plays is reached with a seek: no stream switch,
// nothing breaks in the renderer's output (ADR-016).
func PlayIndex(i int) error {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	mu.Lock()
	at, inStream := streamTimeLocked(i, 0)
	// only while the renderer is known to be on that stream: a hung one (stalled) or one
	// that reports another stream gets a real start
	onIt := lastTrackURI == "" || lastTrackURI == runURI
	inStream = inStream && onIt && wantPlaying && !stalled && pendingSince.IsZero() && resumeAt < 0
	wasPaused := paused
	mu.Unlock()
	if inStream {
		if err := jumpInStream(i, at, wasPaused); err == nil {
			setErr(nil)
			return nil
		}
	}
	err := startLocked(i, 0)
	setErr(err)
	return err
}

// jumpInStream seeks the renderer to track i at stream time at; call under cmdMu.
func jumpInStream(i int, at float64, wasPaused bool) error {
	d, ok := device()
	if !ok {
		return fmt.Errorf("no renderer selected")
	}
	c, cancel := ctx()
	defer cancel()
	if err := upnp.Seek(c, d, at); err != nil {
		return err
	}
	if wasPaused {
		if err := upnp.Play(c, d); err != nil {
			return err
		}
	}
	mu.Lock()
	index, paused, position, streamPos = i, false, 0, at
	duration = trackDuration(i)
	lastCmd = time.Now()
	mu.Unlock()
	savePlaylist()
	return nil
}
