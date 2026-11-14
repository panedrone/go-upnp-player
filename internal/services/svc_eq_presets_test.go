package services

import (
	"slices"
	"strings"
	"testing"
)

func TestEQPresets(t *testing.T) {
	for _, name := range EQPresetNames {
		g, ok := EQPreset(name)
		if !ok || len(g) != len(EQBands) {
			t.Fatalf("%s: ok %v, %d gains", name, ok, len(g))
		}
		for i, v := range g {
			if v < -presetLimit || v > presetLimit || v != float64(int(v)) {
				t.Fatalf("%s band %d: %v, want a whole dB within ±%d", name, i, v, presetLimit)
			}
		}
	}
	flat, _ := EQPreset("Flat")
	if slices.ContainsFunc(flat, func(v float64) bool { return v != 0 }) {
		t.Fatalf("Flat %v", flat)
	}
	// Rock passes through its anchor points: 55 Hz +4, 1 kHz -3, 14 kHz +10 and beyond
	rock, _ := EQPreset("Rock")
	at := func(hz float64) float64 { return rock[slices.Index(EQBands, hz)] }
	if at(55) != 4 || at(110) != 4 || at(14080) != 10 || at(19912) != 10 {
		t.Fatalf("Rock %v", rock)
	}
	// Bass: +8 at the lowest band, nothing from 250 Hz up
	bass, _ := EQPreset("Bass")
	if bass[0] != 8 || bass[len(bass)-1] != 0 {
		t.Fatalf("Bass %v", bass)
	}
	if _, ok := EQPreset("Jazz"); ok {
		t.Fatal("an unknown preset")
	}
}

func TestFeqRoundTrip(t *testing.T) {
	rock, _ := EQPreset("Rock")
	text := FormatFeq(rock)
	if !strings.HasSuffix(text, "\r\n") || strings.Count(text, "\r\n") != len(EQBands) {
		t.Fatalf("%q", text)
	}
	back, err := ParseFeq(text)
	if err != nil || !slices.Equal(back, rock) {
		t.Fatalf("%v %v", back, err)
	}
	// no final line break, LF only, a value past the limit
	lines := strings.Split(strings.TrimRight(FormatFeq(make([]float64, len(EQBands))), "\r\n"), "\r\n")
	lines[0] = "25"
	g, err := ParseFeq(strings.Join(lines, "\n"))
	if err != nil || g[0] != EQMaxGain {
		t.Fatalf("%v %v", g, err)
	}
	for _, bad := range []string{"", "1\r\n2\r\n", strings.Repeat("x\r\n", len(EQBands))} {
		if _, err := ParseFeq(bad); err == nil {
			t.Fatalf("%q parsed", bad)
		}
	}
}

func TestFmtBand(t *testing.T) {
	for hz, want := range map[float64]string{55: "55", 880: "880", 1245: "1.2k", 4978: "5k", 19912: "20k"} {
		if got := FmtBand(hz); got != want {
			t.Errorf("FmtBand(%v) = %q, want %q", hz, got, want)
		}
	}
}
