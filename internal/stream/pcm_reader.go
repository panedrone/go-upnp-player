package stream

import (
	"fmt"
	"io"
	"os"

	"github.com/panedrone/go-upnp-player/internal/services"
)

// pcmBlock is how many samples per channel one read of a WAV file gives.
const pcmBlock = 4096

// pcmSource reads the samples of a WAV file from a given sample on: no decoding, any
// sample is a seek away.
type pcmSource struct {
	f    *os.File
	w    services.WAVFile
	next uint64 // the next sample to read
	path string
}

func openPCM(span Span, from uint64) (*pcmSource, error) {
	f, err := os.Open(span.Path)
	if err != nil {
		return nil, err
	}
	return &pcmSource{f: f, w: *span.WAV, next: from, path: span.Path}, nil
}

// block reads the next pcmBlock samples of every channel.
func (s *pcmSource) block() ([][]int32, error) {
	if s.next >= s.w.Frames {
		return nil, io.EOF
	}
	n := min(uint64(pcmBlock), s.w.Frames-s.next)
	ba := s.w.BlockAlign()
	b := make([]byte, int64(n)*ba)
	got, err := s.f.ReadAt(b, s.w.Data+int64(s.next)*ba)
	switch {
	case got == len(b):
	case err != nil && err != io.EOF:
		return nil, fmt.Errorf("wav %s: %w", s.path, err)
	case int64(got) < ba:
		return nil, io.EOF
	default:
		b = b[:int64(got)/ba*ba] // the file is shorter than its header says
	}
	out := s.w.WAVSamples(b)
	s.next += uint64(len(out[0]))
	return out, nil
}

func (s *pcmSource) Close() error { return s.f.Close() }
