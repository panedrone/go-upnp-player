package player

import (
	"fmt"
	"github.com/panedrone/go-upnp-player/internal/stream"
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
	_, inStream := streamTimeLocked(i, 0)
	// only while the renderer is known to be on that stream: a hung one (stalled) or one
	// that reports another stream gets a real start
	onIt := lastTrackURI == "" || lastTrackURI == runURI
	inStream = inStream && onIt && wantPlaying && !stalled && pendingSince.IsZero() && resumeAt < 0
	mu.Unlock()
	if inStream {
		err := jumpLocked(i, 0)
		setErr(err)
		return err
	}
	err := startLocked(i, 0)
	setErr(err)
	return err
}

// jumpLocked takes the renderer to track i at sec while it plays, the way the owner does
// it by hand without a sound on the FiiO: out of play first, then a start of that track.
// It is a transition of its own, not the Stop button (ADR-025): the renderer is paused on
// silence (quiet), its output stays open, and the player's intent to play is untouched;
// the start then restarts the stream in place, or opens a new one. A seek over playing
// music made the FiiO SR11 stutter, a pause and a seek without letting go of the music
// still jerked once (2026-11-14). Every step waits for the renderer's own report
// (confirm). Call under cmdMu.
func jumpLocked(i int, sec float64) error {
	d, ok := device()
	if !ok {
		return fmt.Errorf("no renderer selected")
	}
	mu.Lock()
	cur, at := runURI, streamPos
	nextURL, nextRun = "", runPlan{}
	mu.Unlock()
	c, cancel := ctx()
	err := quiet(c, d, cur, at)
	cancel()
	if err != nil {
		return err
	}
	return startLocked(i, sec)
}

// renderState is one poll of the renderer, with whether its position stood still since
// the previous poll.
type renderState struct {
	state string
	pos   float64
	still bool
}

// confirmTries bounds every confirmed step: polls, pollEvery apart.
const (
	confirmTries = 50
	pollEvery    = 200 * time.Millisecond
)

// confirmAt polls until the renderer shows it took a seek to at, sent at since: it reports
// that position, standing still, or it has requested the stream from there (a seek is a
// request from the target). The BeagleBone, paused, takes the seek and fetches from the
// target but goes on reporting its old position until Play: waiting for the position
// alone ran out the 50 polls on every jump, some 8 s (2026-11-14). uri is the stream the
// renderer holds: after a switch it is not the player's current run.
func confirmAt(d upnp.Device, uri string, at float64, since time.Time) bool {
	near := func(p float64) bool { return p >= at-1.5 && p <= at+1.5 }
	id, haveRun := runIDOf(uri)
	return confirm(d, func(st renderState) bool {
		if haveRun {
			if p, ok := stream.OpenedSince(id, d.IP, since); ok && near(p) {
				return true
			}
		}
		return near(st.pos) && st.still
	})
}

// confirm polls the renderer until ok holds for what it reports, or confirmTries polls
// have gone by.
func confirm(d upnp.Device, ok func(renderState) bool) bool {
	prev, have := 0.0, false
	for range confirmTries {
		c, cancel := ctx()
		ti, err1 := getTransportInfo(c, d)
		pi, err2 := getPositionInfo(c, d)
		cancel()
		if err1 == nil && err2 == nil {
			st := renderState{state: ti.State, pos: pi.Position, still: have && pi.Position == prev}
			if ok(st) {
				return true
			}
			prev, have = pi.Position, true
		}
		sleep(pollEvery)
	}
	return false
}
