package services

import (
	"testing"
	"time"
)

func TestRacing(t *testing.T) {
	sec := time.Second
	cases := []struct {
		state    string
		advanced float64
		elapsed  time.Duration
		want     bool
	}{
		{"PLAYING", 1, sec, false},      // real time
		{"PLAYING", 5, sec, false},      // a poll that came late, within the margin
		{"PLAYING", 38, sec, true},      // the BeagleBone racing, as measured
		{"PLAYING", 12, 3 * sec, false}, // three seconds between polls
		{"PLAYING", -200, sec, false},   // a seek back or a track change
		{"PAUSED_PLAYBACK", 38, sec, false},
		{"PLAYING", 38, 0, false}, // no previous poll
	}
	for _, c := range cases {
		if got := Racing(c.state, c.advanced, c.elapsed); got != c.want {
			t.Errorf("Racing(%s, %v, %v) = %v", c.state, c.advanced, c.elapsed, got)
		}
	}
}

func TestStalled(t *testing.T) {
	if Stalled("PLAYING", StallAfter-1) {
		t.Fatal("stalled too early")
	}
	if !Stalled("PLAYING", StallAfter) {
		t.Fatal("frozen PLAYING must count as stalled")
	}
	if Stalled("PAUSED_PLAYBACK", 10*StallAfter) {
		t.Fatal("a paused renderer is supposed to stand still")
	}
}
