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
	if Stalled("PLAYING", StallAfter-1, 60, 240) {
		t.Fatal("stalled too early")
	}
	if !Stalled("PLAYING", StallAfter, 60, 240) {
		t.Fatal("frozen PLAYING must count as stalled")
	}
	if Stalled("PLAYING", 10*StallAfter, 253, 253) {
		t.Fatal("frozen at the very end is a track that has ended (FiiO SR11, 2026-11-10)")
	}
	if Stalled("PAUSED_PLAYBACK", 10*StallAfter, 60, 240) {
		t.Fatal("a paused renderer is supposed to stand still")
	}
}

func TestRetryStart(t *testing.T) {
	cases := []struct {
		pending, sinceTry time.Duration
		retry, giveUp     bool
	}{
		{0, 0, false, false},
		{time.Second, time.Second, false, false},
		{3 * time.Second, StartRetryEvery, true, false},
		{StartRetryFor, StartRetryEvery, false, true},
	}
	for _, c := range cases {
		if r, g := RetryStart(c.pending, c.sinceTry); r != c.retry || g != c.giveUp {
			t.Errorf("RetryStart(%v, %v) = %v, %v", c.pending, c.sinceTry, r, g)
		}
	}
}

func TestFromSample(t *testing.T) {
	cases := []struct {
		start, end uint64
		ms         int64
		want       uint64
	}{
		{1000, 100000, 0, 1000},
		{1000, 100000, 1000, 1000 + 44100},
		{1000, 44000, 1000, 1000}, // past the end: the whole track
		{1000, 100000, -5, 1000},
	}
	for _, c := range cases {
		if got := FromSample(c.start, c.end, 44100, c.ms); got != c.want {
			t.Errorf("FromSample(%d, %d, %d) = %d, want %d", c.start, c.end, c.ms, got, c.want)
		}
	}
}
