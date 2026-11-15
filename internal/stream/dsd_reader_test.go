package stream

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// writeTestDSF writes a DSD64 stereo DSF of a sine (a 2nd-order sigma-delta), 1-bit samples
// lowest bit first in blocks of 4096 bytes, as Sony's format has it.
func writeTestDSF(t *testing.T, seconds float64) (string, services.DSDFile) {
	t.Helper()
	const rate, block = 2822400, 4096
	n := int(rate * seconds)
	nBytes := (n + 7) / 8
	blocks := (nBytes + block - 1) / block
	data := make([]byte, blocks*block*2)
	for ch := range 2 {
		var s1, s2, y float64
		for i := range n {
			x := 0.4 * math.Sin(2*math.Pi*float64(500*(ch+1))*float64(i)/rate)
			s1 += x - y
			s2 += s1 - y
			y = -1
			if s2 >= 0 {
				y = 1
				b := i / 8
				data[(b/block*2+ch)*block+b%block] |= 1 << (i % 8)
			}
		}
	}
	le := binary.LittleEndian
	head := make([]byte, 92)
	copy(head, "DSD ")
	le.PutUint64(head[4:], 28)
	le.PutUint64(head[12:], uint64(92+len(data)))
	copy(head[28:], "fmt ")
	le.PutUint64(head[32:], 52)
	le.PutUint32(head[40:], 1)
	le.PutUint32(head[48:], 2)
	le.PutUint32(head[52:], 2)
	le.PutUint32(head[56:], rate)
	le.PutUint32(head[60:], 1)
	le.PutUint64(head[64:], uint64(n))
	le.PutUint32(head[72:], block)
	copy(head[80:], "data")
	le.PutUint64(head[84:], uint64(12+len(data)))
	path := filepath.Join(t.TempDir(), "sine.dsf")
	if err := os.WriteFile(path, append(head, data...), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := services.ParseDSF(head, int64(92+len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return path, d
}

func readSamples(t *testing.T, span Span, offset int64) []int32 {
	t.Helper()
	r, err := Open(span, offset)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if offset == 0 {
		b = b[services.WavHeaderLen:]
	}
	out := make([]int32, len(b)/3)
	for i := range out {
		v := int32(b[3*i]) | int32(b[3*i+1])<<8 | int32(int8(b[3*i+2]))<<16
		out[i] = v
	}
	return out
}

// A DSD stream started in the middle gives the very samples a stream from the start gives
// there (after the start's own 10 ms fade-in), and the length is the file's.
func TestDSDStreamFromTheMiddle(t *testing.T) {
	// the gain is a setting: 0 dB here, whatever the default
	old := library.DSD()
	library.SetDSD(services.DSDSettings{Rate: 88200, GainDB: 0})
	t.Cleanup(func() { library.SetDSD(old) })
	path, d := writeTestDSF(t, 0.5)
	const out = 88200
	n := d.Samples / uint64(d.Rate/out)
	span := Span{Path: path, Start: 0, End: n, SampleRate: out, Channels: 2, Bits: 24, Seekable: true, DSD: &d}
	whole := readSamples(t, span, 0)
	if uint64(len(whole)) != n*2 {
		t.Fatalf("%d samples, want %d", len(whole), n*2)
	}
	peak := int32(0)
	for _, v := range whole[2*2000:] {
		peak = max(peak, v, -v)
	}
	if want := int32(3355443); math.Abs(float64(peak-want)) > float64(want)/50 {
		t.Errorf("peak %d, want about %d", peak, want)
	}
	const k = 20000 // a PCM sample in the middle
	part := readSamples(t, span, int64(services.WavHeaderLen+k*6))
	fade := 2 * (out*fadeMs/1000 + 1)
	for i := fade; i < len(part); i++ {
		if part[i] != whole[2*k+i] {
			t.Fatalf("sample %d from the middle: %d, from the start: %d", i, part[i], whole[2*k+i])
		}
	}
}
