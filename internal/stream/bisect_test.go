package stream

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/mewkiz/flac"
)

func readAll(t *testing.T, span Span, off int64) []byte {
	t.Helper()
	r, err := Open(span, off)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A range request into a file without a SEEKTABLE must return exactly the bytes
// a sequential decode produces at that offset, whether the bisection path or
// the decode-from-start path served it.
func TestSeekWithoutSeekTableMatchesSequentialDecode(t *testing.T) {
	noFade(t) // byte-exact comparisons: see TestStreamStartFadesIn
	path, total := writeTestFLAC(t, 120, 4096, 1000)
	span := Span{Path: path, Start: 0, End: total, SampleRate: 44100, Channels: 2, Bits: 16, Seekable: false}
	full := readAll(t, span, 0)
	if want := span.Layout().TotalLen(); int64(len(full)) != want {
		t.Fatalf("full length %d, want %d", len(full), want)
	}
	const blockAlign = 4
	for _, sample := range []uint64{1, 4095, 4096, 100000, 250000, total - 900, total - 1} {
		for _, extra := range []int64{0, 3} { // also land mid-sample
			off := 44 + int64(sample)*blockAlign + extra
			got := readAll(t, span, off)
			if !bytes.Equal(got, full[off:]) {
				t.Fatalf("sample %d (+%d bytes): %d bytes differ from the sequential decode", sample, extra, len(got))
			}
		}
	}
}

func TestBisectFrameLandsAtOrBeforeTarget(t *testing.T) {
	path, total := writeTestFLAC(t, 120, 4096, 1000)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	st, err := flac.New(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []uint64{100000, 300000, total - 1} {
		off, first, err := bisectFrame(f, st.Info, target)
		if err != nil {
			t.Fatal(err)
		}
		if first > target || target-first > 64*4096 {
			t.Fatalf("target %d: frame starts at sample %d (offset %d)", target, first, off)
		}
	}
}
