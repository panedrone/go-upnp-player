package stream

import (
	"bytes"
	"encoding/binary"
	"io"
	"path/filepath"
	"testing"

	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/testkit"
)

func readRun(t *testing.T, r Run, off int64) []byte {
	t.Helper()
	rd, err := openRun(r, off)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rd.Close() }()
	b, err := io.ReadAll(rd)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// two files, each ending on a frame boundary
func twoFiles(t *testing.T) (Span, Span) {
	t.Helper()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.flac"), filepath.Join(dir, "b.flac")
	na := testkit.WriteFLAC(t, a, testkit.FLAC{Frames: 3, BlockSize: 4096})
	nb := testkit.WriteFLAC(t, b, testkit.FLAC{Frames: 2, BlockSize: 4096})
	return Span{Path: a, End: na, SampleRate: 44100, Channels: 2, Bits: 16},
		Span{Path: b, End: nb, SampleRate: 44100, Channels: 2, Bits: 16}
}

// A run is the tracks' PCM back to back under one header, and any byte offset of it
// can be asked for (a renderer's seek is a Range request).
func TestRunIsTheTracksBackToBack(t *testing.T) {
	noFade(t) // byte-exact comparisons: see TestStreamStartFadesIn
	a, b := twoFiles(t)
	run := Run{Spans: []Span{a, b}}
	whole := readRun(t, run, 0)
	want := append(append([]byte(nil), readAll(t, a, 0)[44:]...), readAll(t, b, 0)[44:]...)
	if !bytes.Equal(whole[44:], want) || int64(len(whole)) != run.Layout().TotalLen() {
		t.Fatalf("run %d bytes, tracks %d", len(whole), len(want))
	}
	if !bytes.Equal(whole[:44], run.Layout().Header()) {
		t.Fatal("the header must describe the whole run")
	}
	for _, off := range []int64{0, 10, 44, 45, 1001, 44 + 3*4096*4 - 1, 44 + 3*4096*4, 44 + 3*4096*4 + 3, int64(len(whole))} {
		if got := readRun(t, run, off); !bytes.Equal(got, whole[off:]) {
			t.Fatalf("from %d: %d bytes, want %d", off, len(got), len(whole)-int(off))
		}
	}
}

// Tracks that continue one another in one file (a CUE image) are decoded as one.
func TestRunMergesAnImage(t *testing.T) {
	path, total := writeTestFLAC(t, 6, 4096, 1000)
	cut := uint64(2*4096 + 300) // a track boundary inside a frame
	one := Span{Path: path, End: cut, SampleRate: 44100, Channels: 2, Bits: 16}
	two := Span{Path: path, Start: cut, End: total, SampleRate: 44100, Channels: 2, Bits: 16}
	run := Run{Spans: []Span{one, two}}
	if p := run.parts(); len(p) != 1 || p[0].Start != 0 || p[0].End != total {
		t.Fatalf("parts %+v", p)
	}
	eqBoost(t)
	whole := Span{Path: path, End: total, SampleRate: 44100, Channels: 2, Bits: 16}
	if !bytes.Equal(readRun(t, run, 0)[44:], readAll(t, whole, 0)[44:]) {
		t.Fatal("an image must play as the whole file, equalizer included")
	}
}

// Over a seam between two files the equalizer goes on as if the music did: its
// filters are not started cold at the second file.
func TestRunEqualizerCarriesOverTheSeam(t *testing.T) {
	noFade(t) // byte-exact comparisons: see TestStreamStartFadesIn
	a, b := twoFiles(t)
	run := Run{Spans: []Span{a, b}}
	flat := readRun(t, run, 0)[44:]
	eqBoost(t)
	got := readRun(t, run, 0)[44:]

	// the reference: the flat PCM through one equalizer, start to end
	n := len(flat) / 4
	chans := [][]int32{make([]int32, n), make([]int32, n)}
	for i := range n {
		chans[0][i] = int32(int16(binary.LittleEndian.Uint16(flat[i*4:])))
		chans[1][i] = int32(int16(binary.LittleEndian.Uint16(flat[i*4+2:])))
	}
	services.NewEQ(*eqSettings.Load(), 44100, 2, 16).Process(chans)
	want := run.Layout().AppendSamples(nil, chans, 0, n)
	if !bytes.Equal(got, want) {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("differs from byte %d of %d (the seam is at %d)", i, len(got), 3*4096*4)
			}
		}
	}
}

func TestRegisterRun(t *testing.T) {
	a, b := twoFiles(t)
	id := RegisterRun(Run{Spans: []Span{a, b}})
	if RegisterRun(Run{Spans: []Span{a, b}}) != id {
		t.Fatal("the same run must get the same id")
	}
	src, ok := ForRun(id)
	if !ok || src.Size != (Run{Spans: []Span{a, b}}).Layout().TotalLen() {
		t.Fatalf("ok %v size %d", ok, src.Size)
	}
	if _, ok := ForRun("nope"); ok {
		t.Fatal("an unknown run")
	}
	for i := range keptRuns {
		RegisterRun(Run{Spans: []Span{{Path: "x", End: uint64(i + 1), SampleRate: 44100, Channels: 2, Bits: 16}}})
	}
	if _, ok := ForRun(id); ok {
		t.Fatal("the oldest runs are let go")
	}
}

// A retired run serves its header and silence, with the same length; restoring it, or
// registering it again, brings the music back.
func TestRetiredRunIsSilence(t *testing.T) {
	a, b := twoFiles(t)
	run := Run{Spans: []Span{a, b}}
	id := RegisterRun(run)
	src, _ := ForRun(id)
	read := func(off int64) []byte {
		rd, err := src.Open(off)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rd.Close() }()
		body, _ := io.ReadAll(rd)
		return body
	}
	music := read(0)
	restore := RetireRun(id)
	quiet := read(0)
	if len(quiet) != len(music) || !bytes.Equal(quiet[:44], music[:44]) || !bytes.Equal(quiet[44:], make([]byte, len(music)-44)) {
		t.Fatalf("silence %d bytes, music %d", len(quiet), len(music))
	}
	if tail := read(int64(len(music) - 10)); !bytes.Equal(tail, make([]byte, 10)) {
		t.Fatal("a Range into a retired run is silence too")
	}
	restore()
	if !bytes.Equal(read(0), music) {
		t.Fatal("restored: music again")
	}
	RetireRun(id)
	RegisterRun(run)
	if !bytes.Equal(read(0), music) {
		t.Fatal("registered again: music again")
	}
}

// noFade turns the fade at stream starts off for a test that compares bytes exactly.
func noFade(t *testing.T) {
	t.Helper()
	fadeMs = 0
	t.Cleanup(func() { fadeMs = 10 })
}

func samples16(b []byte) []int {
	out := make([]int, len(b)/2)
	for i := range out {
		out[i] = int(int16(binary.LittleEndian.Uint16(b[i*2:])))
	}
	return out
}

func abs(v int) int { return max(v, -v) }

// A stream that starts mid-music rises out of silence over its first 10 ms instead of
// stepping into the wave; after that it is the music, sample for sample.
func TestStreamStartFadesIn(t *testing.T) {
	path, total := writeTestFLAC(t, 10, 4096, 1000)
	span := Span{Path: path, End: total, SampleRate: 44100, Channels: 2, Bits: 16}
	off := int64(44 + 5000*4)
	faded := samples16(readAll(t, span, off))
	fadeMs = 0
	plain := samples16(readAll(t, span, off))
	fadeMs = 10
	n := 441 * 2 // 10 ms of stereo samples
	if len(faded) != len(plain) {
		t.Fatalf("%d samples, want %d", len(faded), len(plain))
	}
	for i := range n {
		if abs(faded[i]) > abs(plain[i]) {
			t.Fatalf("sample %d: %d louder than the music's %d", i, faded[i], plain[i])
		}
	}
	if abs(faded[0]) > abs(plain[0])/100+1 {
		t.Fatalf("the first sample %d does not start near silence (music %d)", faded[0], plain[0])
	}
	for i := n; i < len(plain); i++ {
		if faded[i] != plain[i] {
			t.Fatalf("sample %d differs after the fade", i)
		}
	}
}

// Within a run only the start fades: the next part goes on from the previous one.
func TestRunSeamIsNotFaded(t *testing.T) {
	a, b := twoFiles(t)
	run := Run{Spans: []Span{a, b}}
	faded := readRun(t, run, 0)
	fadeMs = 0
	plain := readRun(t, run, 0)
	fadeMs = 10
	head := 44 + 441*4
	if bytes.Equal(faded[:head], plain[:head]) || !bytes.Equal(faded[head:], plain[head:]) {
		t.Fatal("only the first 10 ms of the run may differ")
	}
}
