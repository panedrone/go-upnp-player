package player

import (
	"fmt"
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
}

// PlayIndex jumps to a track in the current queue.
func PlayIndex(i int) error {
	cmdMu.Lock()
	defer cmdMu.Unlock()
	err := startLocked(i, 0)
	setErr(err)
	return err
}
