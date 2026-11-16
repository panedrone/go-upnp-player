package services

import (
	"math"
	"testing"
)

// sigmaDelta makes DSD bytes (most significant bit first) of a sine: a 4th-order modulator,
// enough to keep its noise well below the sine in the band the converter keeps.
func sigmaDelta(dsdRate uint32, freq, amp float64, nBytes int) []byte {
	out := make([]byte, nBytes)
	var s [4]float64
	y := 0.0
	for i := range nBytes * 8 {
		x := amp * math.Sin(2*math.Pi*freq*float64(i)/float64(dsdRate))
		// a chain of integrators with feedback of the 1-bit output (CIFB, stable at amp 0.5)
		s[0] += x - y
		s[1] += s[0] - y
		s[2] += s[1] - y
		s[3] += s[2] - y
		v := s[0]*1 + s[1]*0.5 + s[2]*0.1 + s[3]*0.01
		bit := 0
		y = -1
		if v >= 0 {
			bit, y = 1, 1
		}
		if bit == 1 {
			out[i/8] |= 0x80 >> (i % 8)
		}
	}
	return out
}

// fitSine finds the amplitude and phase of a sine of freq in x (rate fs) and what is left
// after taking it away, as an RMS ratio.
func fitSine(x []int32, fs, freq float64) (amp, phase, residual float64) {
	var sc, ss float64
	for i, v := range x {
		w := 2 * math.Pi * freq * float64(i) / fs
		sc += float64(v) * math.Cos(w)
		ss += float64(v) * math.Sin(w)
	}
	n := float64(len(x))
	a, b := 2*ss/n, 2*sc/n // x ≈ a sin + b cos
	amp = math.Hypot(a, b) / 8388607
	phase = math.Atan2(b, a)
	var res, sig float64
	for i, v := range x {
		w := 2 * math.Pi * freq * float64(i) / fs
		e := float64(v) - (a*math.Sin(w) + b*math.Cos(w))
		res += e * e
		sig += float64(v) * float64(v)
	}
	return amp, phase, math.Sqrt(res / sig)
}

func TestDSDConverterSine(t *testing.T) {
	for _, tc := range []struct{ dsd, out uint32 }{
		{2822400, 44100}, {2822400, 88200}, {2822400, 176400}, {5644800, 88200}, {5644800, 176400},
		// the other family: through the resampler
		{2822400, 48000}, {2822400, 96000}, {2822400, 192000}, {5644800, 192000},
		{3072000, 44100}, {3072000, 176400}, {3072000, 192000},
	} {
		const freq, amp = 1000.0, 0.5
		c := NewDSDConverter(tc.dsd, tc.out, 1, 0)
		cycOut, cycBytes := c.Cycle()
		cycles := int(tc.out/4) / cycOut // a quarter of a second
		nOut := cycles * cycOut
		data := sigmaDelta(tc.dsd, freq, amp, cycles*cycBytes)
		pcm := c.Process([][]byte{data})[0]
		if len(pcm) != nOut {
			t.Fatalf("%d→%d: %d samples, want %d", tc.dsd, tc.out, len(pcm), nOut)
		}
		// skip the filters' run-in, then output k, delayed by Delay, is input time k
		d := c.Delay()
		skip := 2*d + 64
		got := pcm[skip:]
		a, ph, res := fitSine(got, float64(tc.out), freq)
		if math.Abs(a-amp) > 0.01 {
			t.Errorf("%d→%d: amplitude %.4f, want %.2f", tc.dsd, tc.out, a, amp)
		}
		// expected phase: the sample got[0] is input time skip-d samples
		want := 2 * math.Pi * freq * float64(skip-d) / float64(tc.out)
		dp := math.Remainder(ph-want, 2*math.Pi)
		if math.Abs(dp) > 2*math.Pi*freq/float64(tc.out) { // within a sample
			t.Errorf("%d→%d: phase off by %.4f rad (delay %d)", tc.dsd, tc.out, dp, d)
		}
		if res > 0.003 { // -50 dB: the modulator's noise left in the band, nothing else
			t.Errorf("%d→%d: residual %.5f", tc.dsd, tc.out, res)
		}
		if c.Clipped != 0 {
			t.Errorf("%d→%d: %d clipped", tc.dsd, tc.out, c.Clipped)
		}
	}
}

// Feeding the bytes in pieces gives the same samples as all at once: the state carries over,
// the resampler's too.
func TestDSDConverterChunks(t *testing.T) {
	for _, out := range []uint32{176400, 192000} {
		data := sigmaDelta(2822400, 3000, 0.4, 2*4000)
		whole := NewDSDConverter(2822400, out, 2, 3).Process([][]byte{data, data})
		c := NewDSDConverter(2822400, out, 2, 3)
		var parts [2][]int32
		for i := 0; i < len(data); i += 2 * 300 {
			end := min(i+2*300, len(data))
			got := c.Process([][]byte{data[i:end], data[i:end]})
			parts[0] = append(parts[0], got[0]...)
			parts[1] = append(parts[1], got[1]...)
		}
		for ch := range 2 {
			if len(parts[ch]) != len(whole[ch]) {
				t.Fatalf("%d ch %d: %d samples, want %d", out, ch, len(parts[ch]), len(whole[ch]))
			}
			for i := range whole[ch] {
				if parts[ch][i] != whole[ch][i] {
					t.Fatalf("%d ch %d sample %d: %d, want %d", out, ch, i, parts[ch][i], whole[ch][i])
				}
			}
		}
	}
}

// +6 dB on a full-scale SACD sine (50 % modulation) reaches full scale; more clips and is counted.
func TestDSDConverterGainAndClip(t *testing.T) {
	data := sigmaDelta(2822400, 1000, 0.5, 2*44100/4*2)
	c := NewDSDConverter(2822400, 88200, 1, 6)
	pcm := c.Process([][]byte{data})[0]
	a, _, _ := fitSine(pcm[600:], 88200, 1000)
	if math.Abs(a-1) > 0.02 {
		t.Errorf("amplitude at +6 dB %.3f, want 1", a)
	}
	data = sigmaDelta(2822400, 1000, 0.6, 2*44100/4*2)
	c = NewDSDConverter(2822400, 88200, 1, 6)
	c.Process([][]byte{data})
	if c.Clipped == 0 {
		t.Error("a sine above full scale was not counted as clipped")
	}
}

func TestDSDSilenceIsSilent(t *testing.T) {
	data := make([]byte, 2*8820)
	for i := range data {
		data[i] = DSDSilence
	}
	pcm := NewDSDConverter(2822400, 88200, 1, 0).Process([][]byte{data})[0]
	for i, v := range pcm[200:] {
		if v > 8388607/1000 || v < -8388607/1000 { // -60 dB
			t.Fatalf("sample %d: %d", i+200, v)
		}
	}
}

func TestDSDMidRate(t *testing.T) {
	for _, tc := range []struct{ dsd, out, mid uint32 }{
		{2822400, 176400, 176400}, {2822400, 44100, 44100}, {11289600, 88200, 88200},
		{2822400, 192000, 176400}, {2822400, 48000, 44100}, {5644800, 96000, 88200},
		{3072000, 192000, 192000}, {3072000, 176400, 192000}, {3072000, 44100, 48000},
	} {
		if got := DSDMidRate(tc.dsd, tc.out); got != tc.mid {
			t.Errorf("DSDMidRate(%d, %d) = %d, want %d", tc.dsd, tc.out, got, tc.mid)
		}
	}
}

func TestDSDPCMSamples(t *testing.T) {
	// a second of DSD64 is a second of PCM at any rate
	for _, out := range DSDRates {
		if got := DSDPCMSamples(2822400, 2822400, out); got != uint64(out) {
			t.Errorf("one second at %d: %d samples", out, got)
		}
	}
}

func TestDSDSettingsNormalize(t *testing.T) {
	if got := (DSDSettings{Rate: 100000, GainDB: 9}).Normalize(); got != DefaultDSD() {
		t.Errorf("unknown values: %+v", got)
	}
	if got := (DSDSettings{Rate: 88200, GainDB: 3}).Normalize(); got != (DSDSettings{88200, 3}) {
		t.Errorf("known values changed: %+v", got)
	}
}
