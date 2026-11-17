package stream

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/testkit"
)

// A WAV file goes out bit for bit: the stream from any sample on is the file's own PCM
// from there (the fade-in off), under a plain header.
func TestWAVSpanIsTheFilesPCM(t *testing.T) {
	old := fadeMs
	fadeMs = 0
	t.Cleanup(func() { fadeMs = old })
	const frames = 3*pcmBlock + 77
	samples := make([]int32, 2*frames)
	for i := range samples {
		samples[i] = int32((i*7919)%16777216) - 8388608
	}
	w := testkit.WAV{Rate: 96000, Channels: 2, Bits: 24, Samples: samples, Info: map[string]string{"INAM": "x"}}
	path := filepath.Join(t.TempDir(), "a.wav")
	testkit.WriteWAV(t, path, w)
	raw, _ := os.ReadFile(path)
	f, _, err := services.ParseWAV(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	pcm := raw[f.Data : f.Data+int64(frames)*f.BlockAlign()]
	for _, start := range []uint64{0, 1, pcmBlock - 1, 2*pcmBlock + 5} {
		span := Span{Path: path, Start: start, End: frames, SampleRate: 96000, Channels: 2, Bits: 24, Seekable: true, WAV: &f}
		r, err := Open(span, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(got)) != span.Layout().TotalLen() {
			t.Fatalf("from %d: %d bytes, layout says %d", start, len(got), span.Layout().TotalLen())
		}
		if !bytes.Equal(got[services.WavHeaderLen:], pcm[int64(start)*f.BlockAlign():]) {
			t.Errorf("from %d: the PCM differs from the file's", start)
		}
	}
}
