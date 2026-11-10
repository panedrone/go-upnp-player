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
		{"PLAYING", 1, sec, false},     // real time
		{"PLAYING", 2, sec, false},     // RelTime rounding: 1 s of poll can show 2 s
		{"PLAYING", 38, sec, true},     // the BeagleBone racing, as measured once
		{"PLAYING", 4.5, sec, true},    // and another time, ~5x
		{"PLAYING", 4, 3 * sec, false}, // three seconds between polls
		{"PLAYING", -200, sec, false},  // a seek back or a track change
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

func TestStopSafe(t *testing.T) {
	for model, want := range map[string]bool{
		"gmediarender": true,  // stock gmrender-resurrect: the BeagleBone
		"FiiO SR11":    false, // gmrender inside, but Stop hangs it
		"":             false, // unknown: treated like the FiiO
	} {
		if got := StopSafe(model); got != want {
			t.Errorf("StopSafe(%q) = %v", model, got)
		}
	}
}
