package services

import (
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

const sampleCue = `REM GENRE "Hard Rock"
REM DATE 1978
PERFORMER "Black Sabbath"
TITLE "Never Say Die!"
FILE "Black Sabbath - Never Say Die!.wav" WAVE
  TRACK 01 AUDIO
    TITLE "Never Say Die"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Johnny Blade"
    INDEX 00 03:49:70
    INDEX 01 03:50:55
  TRACK 03 AUDIO
    TITLE "Juniors Eyes"
    PERFORMER "Ozzy"
    INDEX 01 10:18:40
`

func TestParseCue(t *testing.T) {
	s, err := ParseCue([]byte(sampleCue))
	if err != nil {
		t.Fatal(err)
	}
	if s.Performer != "Black Sabbath" || s.Title != "Never Say Die!" || s.Date != "1978" || s.Genre != "Hard Rock" {
		t.Fatalf("header: %+v", s)
	}
	if len(s.Files) != 1 || len(s.Files[0].Tracks) != 3 {
		t.Fatalf("files: %+v", s.Files)
	}
	f := s.Files[0]
	if f.Name != "Black Sabbath - Never Say Die!.wav" || f.Type != "WAVE" {
		t.Fatalf("file: %+v", f)
	}
	tr := f.Tracks[1]
	if tr.Number != 2 || tr.Title != "Johnny Blade" || tr.Index00 != (3*60+49)*75+70 || tr.Index01 != (3*60+50)*75+55 {
		t.Fatalf("track 2: %+v", tr)
	}
	if f.Tracks[2].Performer != "Ozzy" {
		t.Fatalf("track performer: %+v", f.Tracks[2])
	}
}

// EAC "tracks with gaps appended": INDEX 01 of track 2 sits under the next FILE.
func TestParseCueGapsAppended(t *testing.T) {
	text := "FILE \"01.wav\" WAVE\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n  TRACK 02 AUDIO\n    TITLE \"Two\"\n    INDEX 00 06:36:36\nFILE \"02.wav\" WAVE\n    INDEX 01 00:00:00\n"
	s, err := ParseCue([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != 2 || len(s.Files[0].Tracks) != 1 || len(s.Files[1].Tracks) != 1 {
		t.Fatalf("files: %+v", s.Files)
	}
	if tr := s.Files[1].Tracks[0]; tr.Number != 2 || tr.Title != "Two" || tr.Index01 != 0 {
		t.Fatalf("carried track: %+v", tr)
	}
}

func TestParseCueDropsTrackWithoutIndex01(t *testing.T) {
	text := "FILE \"a.flac\" WAVE\nTRACK 01 AUDIO\n INDEX 01 00:00:00\nTRACK 02 AUDIO\n INDEX 00 05:00:00\n"
	s, err := ParseCue([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Files[0].Tracks) != 1 {
		t.Fatalf("tracks: %+v", s.Files[0].Tracks)
	}
}

func TestParseCueErrors(t *testing.T) {
	cases := map[string]string{
		"track before file": "TRACK 01 AUDIO\n INDEX 01 00:00:00\n",
		"no index 01":       "FILE \"a.flac\" WAVE\nTRACK 01 AUDIO\n INDEX 00 00:00:00\n",
		"bad time":          "FILE \"a.flac\" WAVE\nTRACK 01 AUDIO\n INDEX 01 00:61:00\n",
	}
	for name, text := range cases {
		if _, err := ParseCue([]byte(text)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestDecodeText(t *testing.T) {
	// a Cyrillic word, written with escapes to keep the source ASCII
	want := "\u041f\u0415\u0421\u041d\u042f"
	cp, _ := charmap.Windows1251.NewEncoder().String(want)
	cases := map[string][]byte{
		"utf8":     []byte(want),
		"utf8 bom": append([]byte{0xEF, 0xBB, 0xBF}, []byte(want)...),
		"cp1251":   []byte(cp),
		"utf16le":  {0xFF, 0xFE, 0x1F, 0x04, 0x15, 0x04, 0x21, 0x04, 0x1D, 0x04, 0x2F, 0x04},
	}
	for name, in := range cases {
		if got := DecodeText(in); got != want {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func TestLayoutCueFile(t *testing.T) {
	s, _ := ParseCue([]byte(sampleCue))
	spans := LayoutCueFile(s.Files[0].Tracks, 44100, 30_000_000)
	if len(spans) != 3 {
		t.Fatalf("spans: %+v", spans)
	}
	// the second track's pregap (INDEX 00) stays in the tail of the first
	if spans[0].Start != 0 || spans[0].End != uint64((3*60+50)*75+55)*588 {
		t.Fatalf("track 1: %+v", spans[0])
	}
	if spans[1].Start != spans[0].End || spans[1].End != uint64((10*60+18)*75+40)*588 {
		t.Fatalf("track 2: %+v", spans[1])
	}
	if spans[2].End != 30_000_000 {
		t.Fatalf("last track must end at file end: %+v", spans[2])
	}
}

func TestFramesToSamples(t *testing.T) {
	for rate, perFrame := range map[uint32]uint64{44100: 588, 48000: 640, 96000: 1280, 192000: 2560, 176400: 2352} {
		if got := FramesToSamples(1, rate); got != perFrame {
			t.Errorf("%d: got %d", rate, got)
		}
	}
}

func TestResolveCueFile(t *testing.T) {
	flac := filepath.Join("d", "album.flac")
	exists := func(p string) bool { return p == flac }
	if got := ResolveCueFile("d", "album.wav", exists, nil); got != flac {
		t.Fatalf("wav->flac fallback: %q", got)
	}
	if got := ResolveCueFile("d", "other.wav", exists, []string{"d/only.flac"}); got != "d/only.flac" {
		t.Fatalf("single flac fallback: %q", got)
	}
	if got := ResolveCueFile("d", "other.wav", exists, []string{"a.flac", "b.flac"}); got != "" {
		t.Fatalf("ambiguous must be empty: %q", got)
	}
}
