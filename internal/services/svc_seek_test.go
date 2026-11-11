package services

import (
	"testing"
	"time"
)

// The cases are what the BeagleBone reported around seeks on 2026-11-11.
func TestSeekLanded(t *testing.T) {
	for _, c := range []struct {
		name     string
		target   float64
		reported float64
		since    time.Duration
		want     bool
	}{
		{"jump of minutes in a run: still the old place", 540, 325, 300 * time.Millisecond, false},
		{"jump of minutes in a run: still the old place a second on", 540, 326, 1200 * time.Millisecond, false},
		{"seek back within a track: the old place", 120, 8, 400 * time.Millisecond, false},
		{"landed: the target", 120, 120, 300 * time.Millisecond, true},
		{"landed and played on", 120, 122, 1500 * time.Millisecond, true},
		{"landed, RelTime rounded down", 120, 119, 200 * time.Millisecond, true},
		{"too far ahead for the time since", 120, 125, 1 * time.Second, false},
		{"seek to the start", 0, 0, 100 * time.Millisecond, true},
		{"seek to the start: the old place", 0, 300, 100 * time.Millisecond, false},
		{"never landed: the renderer is taken at its word", 540, 330, SeekLandWithin, true},
	} {
		if got := SeekLanded(c.target, c.reported, c.since); got != c.want {
			t.Errorf("%s: SeekLanded(%v, %v, %v) = %v, want %v", c.name, c.target, c.reported, c.since, got, c.want)
		}
	}
}

func TestTrackInStream(t *testing.T) {
	starts := []float64{0, 240.4, 420.9}
	for _, c := range []struct {
		name string
		p    float64
		cur  int
		want int
	}{
		{"inside the first track", 100, 0, 0},
		{"inside the second", 300, 1, 1},
		{"the last", 500, 2, 2},
		{"a jump to the third landed a second early", 420, 2, 2},
		{"a jump to the second landed early", 239.5, 1, 1},
		{"the first track playing out: not yet the second", 240, 0, 0},
		{"far before the playing track: the stream is elsewhere", 300, 2, 1},
		{"no hint", 420, -1, 1},
		{"one track", 50, 0, 0},
	} {
		if got := TrackInStream(starts, c.p, c.cur); got != c.want {
			t.Errorf("%s: TrackInStream(%v, %d) = %d, want %d", c.name, c.p, c.cur, got, c.want)
		}
	}
}
