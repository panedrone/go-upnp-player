package services

import "testing"

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
