package stream

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/testkit"
)

// writeTestFLAC generates a FLAC without a SEEKTABLE and with a short last frame.
func writeTestFLAC(t *testing.T, frames, blockSize, lastBlock int) (string, uint64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "noseek.flac")
	return path, testkit.WriteFLAC(t, path, testkit.FLAC{Frames: frames, BlockSize: blockSize, LastBlock: lastBlock})
}

func eqBoost(t *testing.T) {
	t.Helper()
	g := make([]float64, len(services.EQBands))
	g[6] = 12
	SetEQ(services.EQSettings{Enabled: true, Gains: g})
	t.Cleanup(func() { eqSettings.Store(nil) })
}

func TestEQActive(t *testing.T) {
	if EQActive() {
		t.Fatal("active before any settings")
	}
	SetEQ(services.EQSettings{Enabled: true}) // all gains zero: changes nothing
	t.Cleanup(func() { eqSettings.Store(nil) })
	if EQActive() {
		t.Fatal("flat EQ is not active")
	}
	eqBoost(t)
	if !EQActive() {
		t.Fatal("boost must be active")
	}
}

func TestEQChangesSamplesNotLength(t *testing.T) {
	path, total := writeTestFLAC(t, 10, 4096, 1000)
	span := Span{Path: path, End: total, SampleRate: 44100, Channels: 2, Bits: 16}
	flat := readAll(t, span, 0)
	eqBoost(t)
	boosted := readAll(t, span, 0)
	if len(flat) != len(boosted) || bytes.Equal(flat, boosted) {
		t.Fatalf("flat %d, boosted %d bytes", len(flat), len(boosted))
	}
	if !bytes.Equal(flat[:44], boosted[:44]) {
		t.Fatal("the header must not depend on the EQ")
	}
}

func TestOpenEdges(t *testing.T) {
	path, total := writeTestFLAC(t, 4, 4096, 1000)
	span := Span{Path: path, End: total, SampleRate: 44100, Channels: 2, Bits: 16}
	if _, err := Open(span, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err := Open(span, span.Layout().TotalLen()+1); err == nil {
		t.Fatal("offset past the end accepted")
	}
	if got := readAll(t, span, span.Layout().TotalLen()); len(got) != 0 { // no file is opened at all
		t.Fatalf("%d bytes at the end", len(got))
	}
	span.Path = filepath.Join(t.TempDir(), "absent.flac")
	if _, err := Open(span, 0); err == nil {
		t.Fatal("missing file accepted")
	}
	span.Path = filepath.Join(t.TempDir(), "junk.flac")
	if err := os.WriteFile(span.Path, []byte("not a flac at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(span, 0); err == nil {
		t.Fatal("junk accepted")
	}
	span.Seekable = true
	if _, err := Open(span, 1000); err == nil {
		t.Fatal("junk accepted on the seekable path")
	}
}

func TestSubRangeIsCutFromTheFile(t *testing.T) {
	path, total := writeTestFLAC(t, 20, 4096, 4096)
	whole := Span{Path: path, End: total, SampleRate: 44100, Channels: 2, Bits: 16}
	full := readAll(t, whole, 0)
	part := whole
	part.Start, part.End = 5000, 30000
	got := readAll(t, part, 0)
	want := full[44+5000*4 : 44+30000*4]
	if !bytes.Equal(got[44:], want) {
		t.Fatalf("%d bytes differ from the same range of the whole file", len(got)-44)
	}
}

func TestForTrackRawServesTheFileFromOffset(t *testing.T) {
	path, _ := writeTestFLAC(t, 4, 4096, 1000)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := ForTrack(&library.Track{Path: path, FileSize: int64(len(data))}, false)
	if src.Size != int64(len(data)) {
		t.Fatalf("size %d", src.Size)
	}
	r, err := src.Open(100)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[100:]) {
		t.Fatalf("%d bytes", len(got))
	}
	if _, err := ForTrack(&library.Track{Path: path + ".absent"}, false).Open(0); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestForTrackWavAndCueAreDecoded(t *testing.T) {
	path, total := writeTestFLAC(t, 4, 4096, 1000)
	base := library.Track{Path: path, End: total, SampleRate: 44100, Channels: 2, Bits: 16}
	cue := base
	cue.FromCue = true
	for _, src := range []Source{ForTrack(&base, true), ForTrack(&cue, false)} {
		if want := 44 + int64(total)*4; src.Size != want {
			t.Fatalf("size %d, want %d", src.Size, want)
		}
		r, err := src.Open(0)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(r)
		_ = r.Close()
		if int64(len(got)) != src.Size || string(got[:4]) != "RIFF" {
			t.Fatalf("%d bytes", len(got))
		}
	}
}

func TestBisectGivesUpOnJunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0}, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := audioStart(f); err == nil {
		t.Fatal("no FLAC signature, yet an audio start was found")
	}
}
