package services

import "testing"

func TestAvgBitrateKbps(t *testing.T) {
	// the screenshot case: 605 146 592 bytes, 201 823 944 samples at 44.1 kHz -> 1058 kbps
	if got := AvgBitrateKbps(605146592, 201823944, 44100); got != 1058 {
		t.Fatalf("got %d", got)
	}
	for _, c := range []struct {
		size   int64
		frames uint64
		sr     uint32
	}{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-5, 1, 1}} {
		if got := AvgBitrateKbps(c.size, c.frames, c.sr); got != 0 {
			t.Fatalf("%+v: got %d", c, got)
		}
	}
}
