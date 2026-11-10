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
	"github.com/mewkiz/flac/frame"

	"github.com/panedrone/go-upnp-player/internal/services"
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

// fadeMs is how long the start of a stream rises out of silence (see Open).
var fadeMs = 10

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

	// fade: the first this many samples served rise from silence; faded: how many have
	fade, faded int

	// frames, when set, reads frames from a position found by bisection
	// (files without a SEEKTABLE) instead of stream
	frames *bufio.Reader
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
	// A stream that starts here starts the music mid-wave: a step out of silence is a
	// click of its own (the FiiO, 2026-11-11). Its first 10 ms rise instead, as
	// foobar2000 fades in after a seek. A run's next part goes on with no fade.
	r.fade = int(span.SampleRate) * fadeMs / 1000
	// With the equalizer on, decoding starts a little earlier: its filters run in on the
	// music before the first sample served, instead of starting cold on it.
	from := target
	if EQActive() {
		from = max(target-uint64(span.SampleRate)/5, 0)
		if target < uint64(span.SampleRate)/5 {
			from = 0
		}
	}
	if target >= span.End {
		r.done = true
		return r, nil
	}

	f, err := os.Open(span.Path)
	if err != nil {
		return nil, err
	}
	r.file = f
	if span.Seekable && from > 0 {
		st, err := flac.NewSeek(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("flac %s: %w", span.Path, err)
		}
		got, err := st.Seek(from)
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
		// No SEEKTABLE: decoding from the start to a late position took seconds
		// for hi-res files (a 24/192 track: ~6 s to reach 7 minutes), and every
		// track switch seeks to the end of the current track (2026-11-09).
		// Bisect over frame headers instead; on any failure keep the slow path.
		if from > 0 {
			if off, first, err := bisectFrame(f, st.Info, from); err == nil && off > 0 {
				if size, err := fileSize(f); err == nil {
					r.frames = bufio.NewReaderSize(io.NewSectionReader(f, off, size-off), 1<<16)
					r.pos = first
				}
			}
		}
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
	var fr *frame.Frame
	var err error
	if r.frames != nil {
		fr, err = frame.Parse(r.frames)
	} else {
		fr, err = r.stream.ParseNext()
	}
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
		// every frame, the ones before the range too: the filters need continuity
		r.eq.Process(chans)
	}
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
	r.fadeIn(chans, int(from), int(to))
	r.pending = r.layout.AppendSamples(r.pending[:0], chans, int(from), int(to))
	if r.skip > 0 {
		s := min(r.skip, int64(len(r.pending)))
		r.pending = r.pending[s:]
		r.skip -= s
	}
	return nil
}

// fadeIn raises the first served samples [from, to) of a frame out of silence.
func (r *Reader) fadeIn(chans [][]int32, from, to int) {
	for i := from; i < to && r.faded < r.fade; i++ {
		r.faded++
		g := float64(r.faded) / float64(r.fade+1)
		for ch := range chans {
			chans[ch][i] = int32(float64(chans[ch][i]) * g)
		}
	}
}

// Close closes the file.
func (r *Reader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}
