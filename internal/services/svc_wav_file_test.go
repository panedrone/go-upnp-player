package services_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/panedrone/go-upnp-player/internal/services"
	"github.com/panedrone/go-upnp-player/internal/testkit"
)

func TestParseWAV(t *testing.T) {
	w := testkit.WAV{
		Rate: 44100, Channels: 2, Bits: 16, Samples: []int32{1, -1, 300, -300, 32767, -32768},
		Info: map[string]string{"INAM": "Almost Evil", "IART": "Infraction", "ICMT": "odd"},
	}
	b := w.Bytes()
	f, tags, err := services.ParseWAV(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Rate != 44100 || f.Channels != 2 || f.Bits != 16 || f.Frames != 3 {
		t.Errorf("format %+v", f)
	}
	want := map[string]string{"TITLE": "Almost Evil", "ARTIST": "Infraction", "COMMENT": "odd"}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("tags %v, want %v", tags, want)
	}
	got := f.WAVSamples(b[f.Data : f.Data+3*f.BlockAlign()])
	if !reflect.DeepEqual(got, [][]int32{{1, 300, 32767}, {-1, -300, -32768}}) {
		t.Errorf("samples %v", got)
	}
}

// Every integer depth reads back the samples written, 8-bit made signed; the extensible
// header is read as plain PCM.
func TestWAVSamplesDepths(t *testing.T) {
	for _, c := range []struct {
		bits int
		v    []int32
	}{{8, []int32{-128, 127}}, {16, []int32{-32768, 12345}}, {24, []int32{-8388608, 8388607}}, {32, []int32{-2147483648, 7}}} {
		w := testkit.WAV{Rate: 96000, Channels: 1, Bits: c.bits, Samples: c.v, Extensible: c.bits == 24}
		b := w.Bytes()
		f, _, err := services.ParseWAV(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatalf("%d bit: %v", c.bits, err)
		}
		got := f.WAVSamples(b[f.Data : f.Data+int64(len(c.v))*f.BlockAlign()])
		if !reflect.DeepEqual(got[0], c.v) {
			t.Errorf("%d bit: %v, want %v", c.bits, got[0], c.v)
		}
	}
}

func TestParseWAVRefuses(t *testing.T) {
	float := testkit.WAV{Rate: 44100, Channels: 2, Bits: 32, Samples: []int32{0, 0}}.Bytes()
	float[20] = 3 // WAVE_FORMAT_IEEE_FLOAT
	for name, b := range map[string][]byte{"float": float, "not riff": []byte("ID3\x03 not a wave file")} {
		if _, _, err := services.ParseWAV(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A data chunk whose size says more than the file has (cut short) plays what is there.
func TestParseWAVShortData(t *testing.T) {
	b := testkit.WAV{Rate: 44100, Channels: 2, Bits: 16, Samples: make([]int32, 20)}.Bytes()
	b = b[:len(b)-8] // two samples of both channels lost
	f, _, err := services.ParseWAV(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Frames != 8 {
		t.Errorf("frames %d, want 8", f.Frames)
	}
}
