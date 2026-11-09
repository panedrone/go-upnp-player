// Package stream serves a sample range of a FLAC file as a WAV stream.
package stream

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/mewkiz/flac"

	"upnp-player/internal/services"
)

// eqSettings is read by serving streams in the background, hence atomic rather than a plain var.
var eqSettings atomic.Pointer[services.EQSettings]

// SetEQ sets new equalizer settings; open streams pick them up from the next frame.
func SetEQ(s services.EQSettings) {
	s = s.Normalize()
	eqSettings.Store(&s)
}

// EQActive reports whether the equalizer currently changes the sound (then whole FLACs are also served via WAV).
func EQActive() bool {
	p := eqSettings.Load()
	return p != nil && p.Active()
}

// Span is what to cut: a file and a sample range [Start, End).
type Span struct {
	Path       string
	Start, End uint64
	SampleRate uint32
	Channels   uint8
	Bits       uint8
	// Seekable means the file has a SEEKTABLE. Without one mewkiz/flac builds the table
	// by decoding the whole file, so without it we decode from the start and skip.
	Seekable bool
}

// Layout is the geometry of the virtual WAV for this range.
func (s Span) Layout() services.WavLayout {
	return services.NewWavLayout(s.SampleRate, s.Channels, s.Bits, s.End-s.Start)
}

// Reader serves the bytes of the virtual WAV from a given offset to the end.
type Reader struct {
	span   Span
	layout services.WavLayout
	file   *os.File
	stream *flac.Stream

	pending []byte // bytes already prepared but not yet served
	pos     uint64 // absolute sample at which the next frame starts
	skip    int64  // how many bytes of the first sample to drop (Range landed mid-sample)
	cut     uint64 // absolute sample from which we start serving data
	header  []byte
	done    bool

	eqSrc *services.EQSettings // settings the eq was built from
	eq    *services.EQ
}

// Open prepares reading from byte offset offset of the virtual WAV.
func Open(span Span, offset int64) (*Reader, error) {
	layout := span.Layout()
	if offset < 0 || offset > layout.TotalLen() {
		return nil, fmt.Errorf("offset %d out of range", offset)
	}
	r := &Reader{span: span, layout: layout}
	rel, skip, inHeader := layout.LocateOffset(offset)
	if inHeader {
		r.header = layout.Header()[offset:]
	} else {
		r.skip = skip
	}
	target := span.Start + rel
	if target >= span.End {
		r.done = true
		return r, nil
	}

	f, err := os.Open(span.Path)
	if err != nil {
		return nil, err
	}
	r.file = f
	if span.Seekable && target > 0 {
		st, err := flac.NewSeek(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("flac %s: %w", span.Path, err)
		}
		got, err := st.Seek(target)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("flac seek %s: %w", span.Path, err)
		}
		r.stream, r.pos = st, got
	} else {
		st, err := flac.New(bufio.NewReaderSize(f, 1<<16))
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("flac %s: %w", span.Path, err)
		}
		r.stream, r.pos = st, 0
	}
	r.cut = target
	return r, nil
}

// Read implements io.Reader.
func (r *Reader) Read(p []byte) (int, error) {
	n := 0
	if len(r.header) > 0 {
		c := copy(p, r.header)
		r.header = r.header[c:]
		n += c
	}
	for n < len(p) {
		if len(r.pending) == 0 {
			if r.done {
				break
			}
			if err := r.fill(); err != nil {
				if n > 0 {
					return n, nil
				}
				return 0, err
			}
			continue
		}
		c := copy(p[n:], r.pending)
		r.pending = r.pending[c:]
		n += c
	}
	if n == 0 && r.done && len(r.pending) == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// fill decodes the next frame into pending.
func (r *Reader) fill() error {
	if r.pos >= r.span.End {
		r.done = true
		return nil
	}
	fr, err := r.stream.ParseNext()
	if err != nil {
		if errors.Is(err, io.EOF) {
			r.done = true
			return nil
		}
		return err
	}
	frameStart := r.pos
	n := uint64(fr.BlockSize)
	r.pos += n
	if r.pos <= r.cut {
		return nil // the whole frame is before the start of the needed range
	}
	from := uint64(0)
	if r.cut > frameStart {
		from = r.cut - frameStart
	}
	to := n
	if frameStart+to > r.span.End {
		to = r.span.End - frameStart
		r.done = true
	}
	chans := make([][]int32, len(fr.Subframes))
	for i, sf := range fr.Subframes {
		chans[i] = sf.Samples
	}
	if p := eqSettings.Load(); p != r.eqSrc {
		r.eqSrc = p
		r.eq = nil
		if p != nil {
			r.eq = services.NewEQ(*p, r.span.SampleRate, len(chans), r.span.Bits)
		}
	}
	if r.eq != nil {
		// the whole frame, not just the served part: the filters need continuity
		r.eq.Process(chans)
	}
	r.pending = r.layout.AppendSamples(r.pending[:0], chans, int(from), int(to))
	if r.skip > 0 {
		s := min(r.skip, int64(len(r.pending)))
		r.pending = r.pending[s:]
		r.skip -= s
	}
	return nil
}

// Close closes the file.
func (r *Reader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}
