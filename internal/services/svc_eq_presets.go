package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Equalizer presets are frequency curves, not gain lists tied to one band grid: a preset is
// the curve evaluated at every band of EQBands. The same curves as the web page's
// (web/src/Equalizer.tsx), for the Qt window (ADR-027).

// EQPresetNames are the presets in the order they are offered.
var EQPresetNames = []string{"Flat", "Bass", "Treble", "Loudness", "Vocal", "Rock"}

// presetLimit: a preset never goes past ±12 dB, well inside EQMaxGain.
const presetLimit = 12

func octaves(hz, ref float64) float64 { return math.Log2(hz / ref) }

func clampPreset(v float64) float64 {
	return math.Round(math.Max(-presetLimit, math.Min(presetLimit, v)))
}

// eqCurve passes through anchor points [Hz, dB], linear in octaves between them; beyond the
// ends it holds the outermost point's value.
func eqCurve(points [][2]float64) func(float64) float64 {
	return func(hz float64) float64 {
		if hz <= points[0][0] {
			return clampPreset(points[0][1])
		}
		for i := 1; i < len(points); i++ {
			if f1, g1 := points[i][0], points[i][1]; hz <= f1 {
				f0, g0 := points[i-1][0], points[i-1][1]
				return clampPreset(g0 + (g1-g0)*octaves(hz, f0)/octaves(f1, f0))
			}
		}
		return clampPreset(points[len(points)-1][1])
	}
}

var eqPresets = map[string]func(float64) float64{
	"Flat": func(float64) float64 { return 0 },
	"Bass": func(hz float64) float64 {
		if hz < 250 {
			return clampPreset(8 - 2.5*math.Max(0, octaves(hz, 55)))
		}
		return 0
	},
	"Treble": func(hz float64) float64 {
		if hz > 2000 {
			return clampPreset(2.5 * octaves(hz, 2000))
		}
		return 0
	},
	"Loudness": func(hz float64) float64 {
		switch {
		case hz < 300:
			return clampPreset(7 - 2.5*math.Max(0, octaves(hz, 55)))
		case hz > 3000:
			return clampPreset(2 * octaves(hz, 3000))
		}
		return 0
	},
	"Vocal": func(hz float64) float64 { return clampPreset(4 - 2.5*math.Abs(octaves(hz, 1500))) },
	// a "smiley" (V): moderate bass, a dip around 1 kHz and strongly boosted highs, the right
	// wing higher than the left (a symmetric V gave too much bass, too little treble, 2026-11-09)
	"Rock": eqCurve([][2]float64{{55, 4}, {110, 4}, {250, 1}, {600, -2}, {1000, -3}, {2000, 0}, {3500, 4}, {6000, 7}, {10000, 9}, {14000, 10}}),
}

// EQPreset is the named preset's gain for every band of EQBands; false for an unknown name.
func EQPreset(name string) ([]float64, bool) {
	f, ok := eqPresets[name]
	if !ok {
		return nil, false
	}
	g := make([]float64, len(EQBands))
	for i, hz := range EQBands {
		g[i] = f(hz)
	}
	return g, true
}

// ParseFeq reads a foobar2000 .feq preset: one integer gain in dB per line, one line per
// band. foobar's 18 bands (55 Hz to 20 kHz, half an octave apart, ±20 dB) are the same grid
// as EQBands, so the file maps onto the bands index by index. Some files lack the final line
// break. Gains past EQMaxGain are clamped.
func ParseFeq(text string) ([]float64, error) {
	var gains []float64
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			return nil, fmt.Errorf("not an equalizer preset: bad value %q", line)
		}
		gains = append(gains, math.Round(math.Max(-EQMaxGain, math.Min(EQMaxGain, v))))
	}
	if len(gains) != len(EQBands) {
		return nil, fmt.Errorf("not an equalizer preset: expected %d values, got %d", len(EQBands), len(gains))
	}
	return gains, nil
}

// FormatFeq writes gains as a foobar2000 .feq preset: one integer per line, CRLF.
func FormatFeq(gains []float64) string {
	var b strings.Builder
	for _, g := range gains {
		b.WriteString(strconv.Itoa(int(math.Round(g))))
		b.WriteString("\r\n")
	}
	return b.String()
}

// FmtBand labels a band: 1245 -> "1.2k", 4978 -> "5k", 19912 -> "20k".
func FmtBand(hz float64) string {
	if hz < 1000 {
		return strconv.Itoa(int(hz))
	}
	k := hz / 1000
	if k < 10 {
		return strconv.FormatFloat(math.Round(k*10)/10, 'f', -1, 64) + "k"
	}
	return strconv.Itoa(int(math.Round(k))) + "k"
}
