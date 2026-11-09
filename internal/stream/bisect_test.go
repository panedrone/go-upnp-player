package stream

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// writeTestFLAC encodes a FLAC file without a SEEKTABLE: stereo, 16-bit,
// verbatim frames of pseudo-random samples (so sync-like byte pairs do occur
// inside audio data), with a short last frame.
func writeTestFLAC(t *testing.T, frames, blockSize, lastBlock int) (string, uint64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "noseek.flac")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	info := &meta.StreamInfo{
		BlockSizeMin: uint16(blockSize), BlockSizeMax: uint16(blockSize),
		SampleRate: 44100, NChannels: 2, BitsPerSample: 16,
	}
	enc, err := flac.NewEncoder(f, info)
	if err != nil {
		t.Fatal(err)
	}
	seed := uint32(12345)
	next := func() int32 {
		seed = seed*1103515245 + 12345
		return int32(int16(seed >> 16))
	}
	var total uint64
	for i := 0; i < frames; i++ {
		n := blockSize
		if i == frames-1 {
			n = lastBlock
		}
		subs := make([]*frame.Subframe, 2)
		for ch := range subs {
			samples := make([]int32, n)
			for j := range samples {
				samples[j] = next()
			}
			subs[ch] = &frame.Subframe{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, NSamples: n, Samples: samples}
		}
		fr := &frame.Frame{
			Header: frame.Header{
				HasFixedBlockSize: true, BlockSize: uint16(n), SampleRate: 44100,
				Channels: frame.ChannelsLR, BitsPerSample: 16,
			},
			Subframes: subs,
		}
		if err := enc.WriteFrame(fr); err != nil {
			t.Fatal(err)
		}
		total += uint64(n)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return path, total
}

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
