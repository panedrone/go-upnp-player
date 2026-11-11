package services

import "time"

// SeekLandWithin is how long after a seek the renderer's position is checked against
// the target. Past it the renderer is taken at its word: it may have dropped the seek.
const SeekLandWithin = 5 * time.Second

// SeekLanded reports whether a position the renderer reports since seconds after a
// seek to target is one after the seek. gmrender keeps reporting where it was until
// the seek is done: ~0.5 s within a track, over a second for a jump of minutes in a
// run's stream (BeagleBone, 2026-11-11). Taken at face value that old position moved
// the slider back and, after a jump to another track of the run, made the poll take
// the jump for a stream that strayed from the playlist and start yet another track.
// RelTime comes in whole seconds and keeps running after the seek, hence the margins.
func SeekLanded(target, reported float64, since time.Duration) bool {
	if since >= SeekLandWithin {
		return true
	}
	return reported >= target-1.5 && reported <= target+since.Seconds()+1.5
}

// trackSlack: RelTime and seek targets are whole seconds, both cut down, so a jump to a
// track's start lands up to a second before it, and is reported so.
const trackSlack = 1.5

// TrackInStream is the track of a run (its stream starts, ascending) that the stream
// position p is in. cur is the track the player has as playing: a p less than
// trackSlack before cur's start is cur's start, not the end of the track before it.
// Read as the track before, a jump to the next track made the poll take the stream for
// one that strayed from the playlist and start the track after it: Next skipped a
// track (2026-11-11).
func TrackInStream(starts []float64, p float64, cur int) int {
	k := 0
	for k+1 < len(starts) && p >= starts[k+1] {
		k++
	}
	if cur == k+1 && cur < len(starts) && starts[cur]-p < trackSlack {
		return cur
	}
	return k
}
