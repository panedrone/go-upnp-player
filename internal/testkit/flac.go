// Package testkit holds what tests of several packages share: generated FLAC
// files, a fake UPnP renderer and isolated user directories. Only tests import it.
package testkit

import (
	"os"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// FLAC describes a file to generate: stereo, 16-bit, 44.1 kHz, verbatim frames of
// pseudo-random samples (so sync-like byte pairs do occur inside audio data).
type FLAC struct {
	Frames    int
	BlockSize int
	LastBlock int // size of the last frame; 0 means BlockSize
	Tags      [][2]string
	Picture   []byte // embedded JPEG cover, if any
}

// WriteFLAC writes the file and returns its length in samples.
func WriteFLAC(t testing.TB, path string, o FLAC) uint64 {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info := &meta.StreamInfo{
		BlockSizeMin: uint16(o.BlockSize), BlockSizeMax: uint16(o.BlockSize),
		SampleRate: 44100, NChannels: 2, BitsPerSample: 16,
	}
	var blocks []*meta.Block
	if len(o.Tags) > 0 {
		blocks = append(blocks, &meta.Block{
			Header: meta.Header{Type: meta.TypeVorbisComment, Length: 1}, // the encoder replaces Length; 0 would write an empty block
			Body:   &meta.VorbisComment{Vendor: "testkit", Tags: o.Tags},
		})
	}
	if o.Picture != nil {
		blocks = append(blocks, &meta.Block{
			Header: meta.Header{Type: meta.TypePicture, Length: 1},
			Body:   &meta.Picture{Type: 3, MIME: "image/jpeg", Data: o.Picture},
		})
	}
	enc, err := flac.NewEncoder(f, info, blocks...)
	if err != nil {
		t.Fatal(err)
	}
	seed := uint32(12345)
	next := func() int32 {
		seed = seed*1103515245 + 12345
		return int32(int16(seed >> 16))
	}
	var total uint64
	for i := 0; i < o.Frames; i++ {
		n := o.BlockSize
		if i == o.Frames-1 && o.LastBlock > 0 {
			n = o.LastBlock
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
	return total
}

// IsolateUserDirs points the user config and cache directories at a temporary
// folder, so that tests never touch the real settings. Call it from TestMain;
// the returned function removes the folder.
func IsolateUserDirs() (cleanup func()) {
	dir, err := os.MkdirTemp("", "go-upnp-player-test")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "HOME"} {
		if err := os.Setenv(k, dir); err != nil {
			panic(err)
		}
	}
	return func() { _ = os.RemoveAll(dir) }
}
