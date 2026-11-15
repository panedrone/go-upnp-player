package services

import "math"

// DSD to PCM, done in the player (ADR-028). Renderers take PCM over HTTP; DSD itself, native or
// packed as DoP, needs a DAC on USB, which a network streamer is not. The PCM rates are the
// 44.1 kHz family up to 176.4 kHz: DSD rates are its multiples, so the conversion is a chain of
// whole-number decimations, and 192 kHz is the S/PDIF limit of the streamers this player is for.

// DSDSettings is how DSD is turned into PCM: the PCM rate and the gain.
type DSDSettings struct {
	Rate   uint32  `json:"rate"`   // one of DSDRates
	GainDB float64 `json:"gainDb"` // one of DSDGains
}

// DSDRates are the PCM rates to choose from.
var DSDRates = []uint32{44100, 88200, 176400}

// DSDGains are the gains to choose from, dB. The SACD's 0 dB is 50 % modulation, -6 dBFS in PCM:
// +6 brings it to full scale and clips loud remasters, 0 never clips.
var DSDGains = []float64{0, 3, 6}

// DefaultDSD is 176.4 kHz and +6 dB, as the owner plays DSD in foobar2000 (its gain default:
// SACD's full level at full scale).
func DefaultDSD() DSDSettings { return DSDSettings{Rate: 176400, GainDB: 6} }

// Normalize puts unknown values back to the defaults.
func (s DSDSettings) Normalize() DSDSettings {
	d := DefaultDSD()
	okRate, okGain := false, false
	for _, r := range DSDRates {
		okRate = okRate || r == s.Rate
	}
	for _, g := range DSDGains {
		okGain = okGain || g == s.GainDB
	}
	if !okRate {
		s.Rate = d.Rate
	}
	if !okGain {
		s.GainDB = d.GainDB
	}
	return s
}

// DSDOutRate is the PCM rate a DSD stream converts to: the DSD rate halved down from /16 while
// it stays above the chosen rate. A DSD rate of the 48 kHz family (3.072 MHz) lands on its own
// family's nearest rate (96 kHz for 88.2), never above the chosen one by more than 48/44.1.
func DSDOutRate(dsdRate, chosen uint32) uint32 {
	out := dsdRate / 16
	for out > chosen+chosen/10 && out%2 == 0 {
		out /= 2
	}
	return out
}

// DSDBytesPerSample is how many DSD bytes of one channel make one PCM sample.
func DSDBytesPerSample(dsdRate, outRate uint32) int { return int(dsdRate / outRate / 8) }

// DSDSilence is the DSD idle pattern: as many ones as zeros, nothing on the output.
const DSDSilence = 0x69

// DSDConverter turns DSD (one byte stream per channel, most significant bit first) into 24-bit
// PCM. The first stage filters the bits through a FIR from per-byte tables and keeps one value
// per byte (a decimation by 8); stages halving the rate follow down to the PCM rate, and a last
// FIR keeps the band below 35 kHz: everything above is the modulator's noise, which DSD pushes
// up there. Linear phase throughout. It keeps state between calls, one converter per stream.
type DSDConverter struct {
	gain   float64
	tables [][256]float64 // the first stage: per byte position, the sum for each byte value
	hist   [][]byte       // per channel, the last len(tables)-1 bytes
	stages []*firStage    // the halvings, then the last low-pass (factor 1)
	delay  int            // the whole chain's delay, output samples
	// Clipped counts the samples that went over full scale and were clipped
	Clipped int
}

type firStage struct {
	taps   []float64
	factor int
	hist   [][]float64 // per channel, the last len(taps)-1 inputs
}

// dsdPassHz is where the music ends for the filters: a PCM rate whose band ends lower (44.1 kHz)
// takes its own Nyquist frequency less a margin.
const dsdPassHz = 35000

// dsdAttenDB is how far each stage pushes down what it must not let through.
const dsdAttenDB = 120

// NewDSDConverter makes a converter for channels channels of DSD at dsdRate into outRate.
// dsdRate/outRate must be a power of two, 16 or more (DSDOutRate gives such rates).
func NewDSDConverter(dsdRate, outRate uint32, channels int, gainDB float64) *DSDConverter {
	c := &DSDConverter{gain: math.Pow(10, gainDB/20)}
	fs := float64(dsdRate)
	pass := math.Min(dsdPassHz, float64(outRate)/2*0.9)

	// stage 1: bits to values at dsdRate/8; what lands on the music's band after it is kept down
	rate1 := fs / 8
	t1 := lowPass(pass, rate1-pass, fs, dsdAttenDB)
	l1 := len(t1)
	n1 := (l1 + 7) / 8 * 8
	t1 = append(t1, make([]float64, n1-l1)...) // zeros at the oldest end: the delay stays (l1-1)/2
	c.tables = make([][256]float64, n1/8)
	for k := range c.tables {
		for v := range 256 {
			sum := 0.0
			for b := range 8 {
				// bit b of the byte (from the top, the oldest first) is 8k+7-b samples back
				// from the newest input
				x := -1.0
				if (v>>(7-b))&1 == 1 {
					x = 1
				}
				sum += t1[8*k+7-b] * x
			}
			c.tables[k][v] = sum
		}
	}
	c.hist = make([][]byte, channels)
	for ch := range c.hist {
		c.hist[ch] = make([]byte, len(c.tables)-1)
		for i := range c.hist[ch] {
			c.hist[ch][i] = DSDSilence
		}
	}
	// time of output j of a stage, in input bits: step*j + off
	step := 8.0
	off := 7 - float64(l1-1)/2

	rate := rate1
	for rate > float64(outRate) {
		next := rate / 2
		// what folds onto the band below pass is stopped; what folds above it, the last
		// low-pass takes
		taps := lowPass(pass, next-pass, rate, dsdAttenDB)
		c.stages = append(c.stages, newFIR(taps, 2, channels))
		off = step*1 + off - step*float64(len(taps)-1)/2 // output k takes input 2k+1
		step *= 2
		rate = next
	}
	// the last low-pass at the PCM rate, when the halvings left noise above the band
	if rate/2 > pass*1.2 {
		taps := lowPass(pass, math.Min(pass*1.7, rate/2), rate, dsdAttenDB)
		c.stages = append(c.stages, newFIR(taps, 1, channels))
		off -= step * float64(len(taps)-1) / 2
	}
	c.delay = int(math.Round(-off / step))
	return c
}

func newFIR(taps []float64, factor, channels int) *firStage {
	s := &firStage{taps: taps, factor: factor, hist: make([][]float64, channels)}
	for ch := range s.hist {
		s.hist[ch] = make([]float64, len(taps)-1)
	}
	return s
}

// Delay is how many output samples the conversion lags behind its input.
func (c *DSDConverter) Delay() int { return c.delay }

// Process converts the next DSD bytes of every channel (equal lengths, a multiple of
// DSDBytesPerSample) and returns the PCM samples per channel, 24-bit in int32.
func (c *DSDConverter) Process(in [][]byte) [][]int32 {
	out := make([][]int32, len(in))
	for ch, bytes := range in {
		x := c.first(ch, bytes)
		for _, s := range c.stages {
			x = s.run(ch, x)
		}
		pcm := make([]int32, len(x))
		for i, v := range x {
			v *= c.gain
			if v > 1 || v < -1 {
				c.Clipped++
				v = math.Max(-1, math.Min(1, v))
			}
			pcm[i] = int32(math.Round(v * 8388607))
		}
		out[ch] = pcm
	}
	return out
}

// first runs the first stage: one value per input byte.
func (c *DSDConverter) first(ch int, in []byte) []float64 {
	h := c.hist[ch]
	buf := make([]byte, 0, len(h)+len(in))
	buf = append(append(buf, h...), in...)
	n := len(c.tables)
	out := make([]float64, len(in))
	for j := range in {
		// the newest byte is buf[j+n-1]; table k takes the byte k back from it
		sum := 0.0
		for k := range n {
			sum += c.tables[k][buf[j+n-1-k]]
		}
		out[j] = sum
	}
	copy(h, buf[len(buf)-len(h):])
	return out
}

// run filters one channel's block and keeps every factor-th value.
func (s *firStage) run(ch int, in []float64) []float64 {
	h := s.hist[ch]
	buf := make([]float64, 0, len(h)+len(in))
	buf = append(append(buf, h...), in...)
	n := len(s.taps)
	out := make([]float64, 0, len(in)/s.factor)
	for t := s.factor - 1; t < len(in); t += s.factor {
		sum := 0.0
		top := t + n - 1 // the newest input of this output in buf
		for i, k := range s.taps {
			sum += k * buf[top-i]
		}
		out = append(out, sum)
	}
	copy(h, buf[len(buf)-len(h):])
	return out
}

// lowPass designs a linear-phase low-pass FIR passing up to pass and stopping from stop
// (Hz, at sample rate fs) attenDB down: a windowed sinc with a Kaiser window, unity at DC.
func lowPass(pass, stop, fs, attenDB float64) []float64 {
	dw := 2 * math.Pi * (stop - pass) / fs
	n := int(math.Ceil((attenDB-7.95)/(2.285*dw))) + 1
	if n%2 == 0 {
		n++
	}
	beta := 0.1102 * (attenDB - 8.7)
	fc := (pass + stop) / 2 / fs
	taps := make([]float64, n)
	m := float64(n-1) / 2
	sum := 0.0
	for i := range taps {
		x := float64(i) - m
		v := 2 * fc
		if x != 0 {
			v = math.Sin(2*math.Pi*fc*x) / (math.Pi * x)
		}
		r := x / m
		v *= besselI0(beta*math.Sqrt(1-r*r)) / besselI0(beta)
		taps[i] = v
		sum += v
	}
	for i := range taps {
		taps[i] /= sum
	}
	return taps
}

// besselI0 is the modified Bessel function of the first kind, order 0 (for the Kaiser window).
func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 50; k++ {
		term *= (x / 2) / float64(k)
		sum += term * term
		if term*term < sum*1e-17 {
			break
		}
	}
	return sum
}
