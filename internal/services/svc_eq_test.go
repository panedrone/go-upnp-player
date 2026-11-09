package services

import (
	"math"
	"testing"
)

func TestEQInactive(t *testing.T) {
	if NewEQ(EQSettings{Enabled: true, Gains: make([]float64, len(EQBands))}, 44100, 2, 16) != nil {
		t.Fatal("flat EQ must be nil")
	}
	g := make([]float64, len(EQBands))
	g[0] = 6
	if NewEQ(EQSettings{Enabled: false, Gains: g}, 44100, 2, 16) != nil {
		t.Fatal("disabled EQ must be nil")
	}
}

func TestEQBands(t *testing.T) {
	want := []float64{55, 78, 110, 156, 220, 311, 440, 622, 880, 1245, 1760, 2489, 3520, 4978, 7040, 9956, 14080, 19912}
	for i, f := range want {
		if EQBands[i] != f {
			t.Fatalf("band %d: %v", i, EQBands)
		}
	}
}

func TestEQNormalize(t *testing.T) {
	g := make([]float64, len(EQBands))
	g[0], g[1] = 30, -30
	s := EQSettings{Gains: g}.Normalize()
	if s.Gains[0] != EQMaxGain || s.Gains[1] != -EQMaxGain || s.Gains[2] != 0 {
		t.Fatalf("%+v", s)
	}
	// a set for the old grid (10 bands) is reset, not remapped
	old := EQSettings{Enabled: true, Gains: []float64{6, 6, 6, 6, 6, 6, 6, 6, 6, 6}}.Normalize()
	if len(old.Gains) != len(EQBands) || old.Active() {
		t.Fatalf("old grid: %+v", old)
	}
}

// rms of a sine at frequency f after the equalizer (with filter warm-up).
func sineRMS(e *EQ, f float64) float64 {
	const fs, n = 44100, 44100
	buf := make([]int32, n)
	for i := range buf {
		buf[i] = int32(2000 * math.Sin(2*math.Pi*f*float64(i)/fs))
	}
	e.Process([][]int32{buf})
	var sum float64
	for _, v := range buf[n/2:] {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(n/2))
}

func TestEQBoostsBand(t *testing.T) {
	gains := make([]float64, len(EQBands))
	gains[9] = 12 // 1245 Hz
	in := 2000 / math.Sqrt2
	boosted := sineRMS(NewEQ(EQSettings{Enabled: true, Gains: gains}, 44100, 1, 16), 1245)
	// exactly +12 dB at the band center (-24 dBFS + 12 does not reach the limiter)
	if d := 20 * math.Log10(boosted/in); math.Abs(d-12) > 0.5 {
		t.Fatalf("1245 Hz: %.2f dB", d)
	}
	// far from the band the level does not change: no global attenuation
	far := sineRMS(NewEQ(EQSettings{Enabled: true, Gains: gains}, 44100, 1, 16), 60)
	if d := 20 * math.Log10(far/in); math.Abs(d) > 0.5 {
		t.Fatalf("60 Hz: %.2f dB", d)
	}
}

func TestEQCutBand(t *testing.T) {
	gains := make([]float64, len(EQBands))
	gains[9] = -12
	in := 2000 / math.Sqrt2
	cut := sineRMS(NewEQ(EQSettings{Enabled: true, Gains: gains}, 44100, 1, 16), 1245)
	if d := 20 * math.Log10(cut/in); d > -11 {
		t.Fatalf("1 kHz cut: %.2f dB", d)
	}
}

func TestEQClamps(t *testing.T) {
	gains := make([]float64, len(EQBands))
	gains[9] = 12
	e := NewEQ(EQSettings{Enabled: true, Gains: gains}, 44100, 1, 16)
	e.preamp = 4 // deliberate overload
	buf := []int32{32767, -32768, 30000}
	e.Process([][]int32{buf})
	for _, v := range buf {
		if v > 32767 || v < -32768 {
			t.Fatalf("not clamped: %v", buf)
		}
	}
}

// the top band (~19.9 kHz) works even at 44.1 kHz
func TestEQTopBandAt44k(t *testing.T) {
	gains := make([]float64, len(EQBands))
	gains[len(gains)-1] = 12
	e := NewEQ(EQSettings{Enabled: true, Gains: gains}, 44100, 1, 16)
	if e == nil || len(e.filters) != 1 {
		t.Fatal("top band must not be dropped at 44.1 kHz")
	}
	in := 2000 / math.Sqrt2
	// 1 kHz is far from the band: level unchanged
	if d := 20 * math.Log10(sineRMS(e, 1000)/in); math.Abs(d) > 0.5 {
		t.Fatalf("1 kHz: %.2f dB", d)
	}
}

// treble +5/+12/+17/+18 (as on the live screenshot of 2026-11-09) does not make the music quieter
func TestEQTrebleBoostKeepsLevel(t *testing.T) {
	gains := make([]float64, len(EQBands))
	for i, g := range []float64{5, 12, 17, 18} {
		gains[len(gains)-4+i] = g
	}
	e := NewEQ(EQSettings{Enabled: true, Gains: gains}, 44100, 1, 16)
	in := 2000 / math.Sqrt2
	if d := 20 * math.Log10(sineRMS(e, 440)/in); math.Abs(d) > 0.5 {
		t.Fatalf("440 Hz must stay at 0 dB, got %.2f dB", d)
	}
}

func TestSoftLimit(t *testing.T) {
	if softLimit(0.5) != 0.5 || softLimit(-0.5) != -0.5 {
		t.Fatal("below knee must be linear")
	}
	prev := 0.0
	for x := limitKnee; x < 4; x += 0.01 {
		y := softLimit(x)
		if y > 1 || y < prev { // exactly 1.0 is tanh saturation in float64, clamped beyond that
			t.Fatalf("x=%.2f y=%.4f", x, y)
		}
		prev = y
	}
	if softLimit(-3) != -softLimit(3) {
		t.Fatal("must be symmetric")
	}
}
