package stream

import (
	"bufio"
	"io"
	"os"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// Source is a virtual track file: its size and opening from a byte offset.
type Source struct {
	Size int64
	Open func(offset int64) (io.ReadCloser, error)
}

// ForTrack: wav=true means decoding (and the equalizer) to WAV; otherwise the FLAC itself as is.
// A cue track is always WAV: otherwise the range cannot be cut out. fromMs > 0 starts
// the stream that far into the track (always WAV), see services.FromSample.
func ForTrack(t *library.Track, wav bool, fromMs int64) Source {
	if !wav && !t.FromCue && fromMs <= 0 {
		return Source{Size: t.FileSize, Open: func(offset int64) (io.ReadCloser, error) {
			return openRaw(t.Path, offset)
		}}
	}
	span := Span{
		Path: t.Path, Start: services.FromSample(t.Start, t.End, t.SampleRate, fromMs), End: t.End,
		SampleRate: t.SampleRate, Channels: t.Channels, Bits: t.Bits, Seekable: t.Seekable,
	}
	return Source{Size: span.Layout().TotalLen(), Open: func(offset int64) (io.ReadCloser, error) {
		return Open(span, offset)
	}}
}

type rawFile struct {
	*bufio.Reader
	f *os.File
}

func (r rawFile) Close() error { return r.f.Close() }

func openRaw(path string, offset int64) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	return rawFile{Reader: bufio.NewReaderSize(f, 1<<16), f: f}, nil
}
