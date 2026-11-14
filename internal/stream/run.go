package stream

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// A run is several tracks of one format served as one WAV: the renderer plays it as
// one song and never switches streams between them. A switch is where the FiiO SR11
// let its coaxial output run dry and the DAC clicked (ADR-016).

// MaxRunData caps the PCM of one run: the RIFF header counts its sizes in 32 bits.
const MaxRunData = 0xF000_0000

// TrackSpan is the sample range of a track to serve, fromMs into it.
func TrackSpan(t *library.Track, fromMs int64) Span {
	return Span{
		Path: t.Path, Start: services.FromSample(t.Start, t.End, t.SampleRate, fromMs), End: t.End,
		SampleRate: t.SampleRate, Channels: t.Channels, Bits: t.Bits, Seekable: t.Seekable,
	}
}

// SameFormat reports whether two spans can follow each other in one WAV.
func SameFormat(a, b Span) bool {
	return a.SampleRate == b.SampleRate && a.Channels == b.Channels && a.Bits == b.Bits
}

// Run is the spans of a run, in order, all of one format.
type Run struct {
	Spans []Span
	// Lead: samples at the run's start that the renderer's first request (from the
	// stream's start) gets as silence. A start in the middle of an album opens the run
	// at the album's first track, so that a jump back within it is a seek, not a new
	// stream; until the player's seek to the chosen track lands, the tracks before it
	// are not to be heard. Every later request (a seek is a Range request) gets the
	// music (ADR-020).
	Lead uint64
}

// Layout is the geometry of the run's WAV.
func (r Run) Layout() services.WavLayout {
	var n uint64
	for _, s := range r.Spans {
		n += s.End - s.Start
	}
	f := r.Spans[0]
	return services.NewWavLayout(f.SampleRate, f.Channels, f.Bits, n)
}

// parts merges spans that continue one another in the same file (the tracks of a
// CUE image): those are decoded as one, with no seam at all.
func (r Run) parts() []Span {
	out := make([]Span, 0, len(r.Spans))
	for _, s := range r.Spans {
		if n := len(out); n > 0 && out[n-1].Path == s.Path && out[n-1].End == s.Start {
			out[n-1].End = s.End
			out[n-1].Seekable = out[n-1].Seekable && s.Seekable
			continue
		}
		out = append(out, s)
	}
	return out
}

var (
	runsMu   sync.Mutex
	runs     = map[string]Run{}
	runOrder []string
	// retired runs serve silence: the renderer is being moved off them (RetireRun)
	retired = map[string]bool{}
	// the latest request for each run: where in the stream it started and when
	lastOpen = map[string]openedAt{}
)

type openedAt struct {
	sec float64
	at  time.Time
}

// OpenedSince reports where in the run's stream, in seconds, the renderer's latest request
// started, if it came after since. A seek is a request from the target: the renderer's
// own sign that it took the seek, even where it does not report the new position while
// paused (the BeagleBone, 2026-11-14).
func OpenedSince(id string, since time.Time) (float64, bool) {
	runsMu.Lock()
	defer runsMu.Unlock()
	o, ok := lastOpen[id]
	if !ok || o.at.Before(since) {
		return 0, false
	}
	return o.sec, true
}

// keptRuns: the renderer may come back for a run it is playing (a seek is a Range
// request), and the next one is queued ahead; a few more than that is plenty.
const keptRuns = 16

// RegisterRun keeps a run for serving and returns its id. The same spans give the
// same id.
func RegisterRun(r Run) string {
	h := sha256.New()
	for _, s := range r.Spans {
		_, _ = fmt.Fprintf(h, "%s|%d|%d\n", s.Path, s.Start, s.End)
	}
	id := hex.EncodeToString(h.Sum(nil))[:16]
	runsMu.Lock()
	defer runsMu.Unlock()
	delete(retired, id) // registered again: it is to be played
	if _, ok := runs[id]; ok {
		runs[id] = r // the same tracks with this start's lead
	} else {
		runs[id] = r
		runOrder = append(runOrder, id)
		if len(runOrder) > keptRuns {
			delete(runs, runOrder[0])
			delete(retired, runOrder[0])
			runOrder = runOrder[1:]
		}
	}
	return id
}

// RetireRun lets a run go: from now on its data is served as silence, with the same
// length. A renderer is moved off a stream through its gapless path, which plays the
// last second of that stream first (ADR-003): the end of another song, heard for a
// moment. Silence instead, and nothing else changes. restore takes it back, for a
// switch that did not happen.
func RetireRun(id string) (restore func()) {
	runsMu.Lock()
	defer runsMu.Unlock()
	if _, ok := runs[id]; !ok || retired[id] {
		return func() {}
	}
	retired[id] = true
	return func() {
		runsMu.Lock()
		defer runsMu.Unlock()
		delete(retired, id)
	}
}

// ForRun is the source of a registered run.
func ForRun(id string) (Source, bool) {
	runsMu.Lock()
	r, ok := runs[id]
	runsMu.Unlock()
	if !ok || len(r.Spans) == 0 {
		return Source{}, false
	}
	return Source{Size: r.Layout().TotalLen(), Open: func(offset int64) (io.ReadCloser, error) {
		layout := r.Layout()
		sec := 0.0
		if data := offset - services.WavHeaderLen; data > 0 {
			sec = float64(data/layout.BlockAlign()) / float64(r.Spans[0].SampleRate)
		}
		runsMu.Lock()
		silent := retired[id]
		lastOpen[id] = openedAt{sec, time.Now()}
		runsMu.Unlock()
		if silent {
			return openSilence(r, offset)
		}
		if r.Lead > 0 && offset < services.WavHeaderLen {
			return openWithLead(r, offset)
		}
		return openRun(r, offset)
	}}, true
}

// openSilence serves the run's header and zero samples in place of its data.
func openSilence(r Run, offset int64) (io.ReadCloser, error) {
	layout := r.Layout()
	if offset < 0 || offset > layout.TotalLen() {
		return nil, fmt.Errorf("offset %d out of range", offset)
	}
	var head []byte
	if offset < services.WavHeaderLen {
		head = layout.Header()[offset:]
	}
	data := layout.TotalLen() - max(offset, services.WavHeaderLen)
	return io.NopCloser(io.MultiReader(bytes.NewReader(head), io.LimitReader(zeros{}, data))), nil
}

// openWithLead serves a request from the stream's start: the header, silence for the
// lead, then the music from there, faded in as any stream start is.
func openWithLead(r Run, offset int64) (io.ReadCloser, error) {
	layout := r.Layout()
	leadBytes := int64(r.Lead) * int64(layout.BlockAlign())
	music := services.WavHeaderLen + leadBytes
	if music >= layout.TotalLen() {
		return openSilence(r, offset)
	}
	rest, err := openRun(r, music)
	if err != nil {
		return nil, err
	}
	head := layout.Header()[offset:]
	return readCloser{io.MultiReader(bytes.NewReader(head), io.LimitReader(zeros{}, leadBytes), rest), rest}, nil
}

type readCloser struct {
	io.Reader
	c io.Closer
}

func (r readCloser) Close() error { return r.c.Close() }

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// runReader serves the run's WAV: its own header, then the parts one after another.
type runReader struct {
	header []byte
	parts  []Span
	next   int // the part to open after cur
	cur    *Reader
	skip   int64 // bytes to drop from the first part (a Range landing mid-sample)
	first  int64 // sample offset into the first part opened
	// handoff: the part just finished, whose equalizer the next part continues
	handoff *Reader
	opened  bool // a part was opened already: the next ones continue it
}

func openRun(r Run, offset int64) (io.ReadCloser, error) {
	layout := r.Layout()
	if offset < 0 || offset > layout.TotalLen() {
		return nil, fmt.Errorf("offset %d out of range", offset)
	}
	rr := &runReader{parts: r.parts()}
	sample, skip, inHeader := layout.LocateOffset(offset)
	if inHeader {
		rr.header = layout.Header()[offset:]
	} else {
		rr.skip = skip
	}
	for rr.next < len(rr.parts) {
		p := rr.parts[rr.next]
		if n := p.End - p.Start; sample >= n {
			sample -= n
			rr.next++
			continue
		}
		rr.first = int64(sample)
		break
	}
	return rr, nil
}

func (r *runReader) Read(p []byte) (int, error) {
	n := 0
	if len(r.header) > 0 {
		c := copy(p, r.header)
		r.header = r.header[c:]
		n += c
	}
	for n < len(p) {
		if r.cur == nil {
			if r.next >= len(r.parts) {
				break
			}
			if err := r.open(); err != nil {
				if n > 0 {
					return n, nil
				}
				return 0, err
			}
		}
		c, err := r.cur.Read(p[n:])
		n += c
		if err == io.EOF {
			r.done()
			continue
		}
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// open opens the next part right after its header, at the first sample to serve.
func (r *runReader) open() error {
	p := r.parts[r.next]
	ba := p.Layout().BlockAlign()
	rd, err := Open(p, services.WavHeaderLen+r.first*ba+r.skip)
	if err != nil {
		return err
	}
	if h := r.handoff; h != nil {
		rd.eq, rd.eqSrc = h.eq, h.eqSrc
		r.handoff = nil
	}
	if r.opened {
		rd.fade = 0 // the music goes on from the previous part: no fade
	}
	r.opened = true
	r.first, r.skip = 0, 0
	r.cur = rd
	r.next++
	return nil
}

// done closes the current part. Its equalizer carries over to the next part when
// that is a different file played from its start: the filters go on as if the
// music did, with no cold start at the seam. A jump within one file starts afresh.
func (r *runReader) done() {
	prev := r.cur
	_ = prev.Close()
	r.cur = nil
	if r.next < len(r.parts) {
		nx := r.parts[r.next]
		if nx.Path != prev.span.Path && nx.Start == 0 && r.first == 0 {
			r.handoff = prev
		}
	}
}

func (r *runReader) Close() error {
	if r.cur != nil {
		return r.cur.Close()
	}
	return nil
}
