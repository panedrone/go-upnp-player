package services

import "math"

// EQBands are 18 half-octave bands: 55·2^(n/2) Hz, from 55 Hz to ~20 kHz.
var EQBands = func() []float64 {
	b := make([]float64, 18)
	for n := range b {
		b[n] = math.Round(55 * math.Pow(2, float64(n)/2))
	}
	return b
}()

// EQMaxGain is the band boost/cut limit, dB.
const EQMaxGain = 20.0

// eqQ is the band Q factor. For a half octave the "proper" Q≈2.87, but then boosting
// adjacent bands leaves dips between them; 2.0 gives a flat sum.
const eqQ = 2.0

// EQSettings is what the user adjusts.
type EQSettings struct {
	Enabled bool      `json:"enabled"`
	Gains   []float64 `json:"gains"` // dB, one per band in EQBands
}

// Normalize clamps gains to the allowed range. A set of a different length (saved for
// an older band grid) is not remapped: the bands do not match, so it is reset to zero.
func (s EQSettings) Normalize() EQSettings {
	g := make([]float64, len(EQBands))
	if len(s.Gains) == len(EQBands) {
		for i := range g {
			g[i] = math.Max(-EQMaxGain, math.Min(EQMaxGain, s.Gains[i]))
		}
	}
	return EQSettings{Enabled: s.Enabled, Gains: g}
}

// Active reports whether the equalizer actually changes the sound.
func (s EQSettings) Active() bool {
	if !s.Enabled {
		return false
	}
	for _, g := range s.Gains {
		if g != 0 {
			return true
		}
	}
	return false
}

type biquad struct{ b0, b1, b2, a1, a2 float64 }

// peaking — RBJ Audio EQ Cookbook, peaking EQ.
func peaking(fs, f0, gainDB, q float64) biquad {
	a := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * f0 / fs
	alpha := math.Sin(w0) / (2 * q)
	cos := math.Cos(w0)
	a0 := 1 + alpha/a
	return biquad{
		b0: (1 + alpha*a) / a0,
		b1: -2 * cos / a0,
		b2: (1 - alpha*a) / a0,
		a1: -2 * cos / a0,
		a2: (1 - alpha/a) / a0,
	}
}

type biquadState struct{ x1, x2, y1, y2 float64 }

// EQ is the filter set for one stream (sample rate and channel count are fixed).
// Filter state carries across frames, so each stream gets its own instance.
type EQ struct {
	filters []biquad
	state   [][]biquadState // [channel][filter]
	preamp  float64         // 1 means no attenuation
	scale   float64         // 2^(bits-1)
}

// limitKnee is the level (fraction of full scale, ≈ −1 dBFS) at which the limiter starts
// rounding off peaks.
const limitKnee = 0.89

// softLimit is a soft limiter in fractions of full scale: linear up to the knee, above it
// tanh rounding, asymptotically approaching 1. No sample ever goes past full scale.
func softLimit(x float64) float64 {
	a := math.Abs(x)
	if a <= limitKnee {
		return x
	}
	y := limitKnee + (1-limitKnee)*math.Tanh((a-limitKnee)/(1-limitKnee))
	return math.Copysign(y, x)
}

// NewEQ returns nil if the equalizer changes nothing.
func NewEQ(s EQSettings, sampleRate uint32, channels int, bits uint8) *EQ {
	s = s.Normalize()
	if !s.Active() || sampleRate == 0 || channels == 0 {
		return nil
	}
	fs := float64(sampleRate)
	e := &EQ{scale: math.Ldexp(1, int(bits)-1), preamp: 1}
	for i, g := range s.Gains {
		f0 := EQBands[i]
		// A bilinear-transform biquad is stable right up to Nyquist, but there the
		// band shrinks to nothing; 0.475·fs lets the top band (~19.9 kHz) through
		// even at 44.1 kHz, without going right up to fs/2.
		if g == 0 || f0 >= fs*0.475 {
			continue
		}
		e.filters = append(e.filters, peaking(fs, f0, g, eqQ))
	}
	if len(e.filters) == 0 {
		return nil
	}
	// There is no preamp cut by the maximum boost: it attenuated EVERYTHING by that many
	// decibels, and a +18 treble boost turned into "all the music at −18 dB, treble as
	// before" (seen live 2026-11-09). A boost stays a boost; softLimit catches overload.
	e.state = make([][]biquadState, channels)
	for ch := range e.state {
		e.state[ch] = make([]biquadState, len(e.filters))
	}
	return e
}

// Process processes samples in place. channels[ch][i].
func (e *EQ) Process(channels [][]int32) {
	maxV := e.scale - 1
	minV := -e.scale
	for ch, samples := range channels {
		if ch >= len(e.state) {
			break
		}
		st := e.state[ch]
		for i, v := range samples {
			x := float64(v) * e.preamp
			for f := range e.filters {
				b, s := &e.filters[f], &st[f]
				y := b.b0*x + b.b1*s.x1 + b.b2*s.x2 - b.a1*s.y1 - b.a2*s.y2
				s.x2, s.x1 = s.x1, x
				s.y2, s.y1 = s.y1, y
				x = y
			}
			x = math.Round(softLimit(x/e.scale) * e.scale)
			if x > maxV {
				x = maxV
			} else if x < minV {
				x = minV
			}
			samples[i] = int32(x)
		}
	}
}
