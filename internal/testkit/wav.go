package testkit

import (
	"encoding/binary"
	"os"
	"testing"
)

// WAV describes a test WAV file: integer PCM with the given samples, interleaved, plus an
// optional LIST INFO chunk (id -> text) placed before the data, as foobar2000 writes it.
type WAV struct {
	Rate     uint32
	Channels int
	Bits     int
	Samples  []int32 // interleaved
	Info     map[string]string
	// Extensible writes a WAVE_FORMAT_EXTENSIBLE fmt chunk
	Extensible bool
}

// Bytes builds the file.
func (w WAV) Bytes() []byte {
	chunk := func(id string, body []byte) []byte {
		out := append([]byte(id), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(out[4:], uint32(len(body)))
		out = append(out, body...)
		if len(body)%2 == 1 {
			out = append(out, 0)
		}
		return out
	}
	ba := w.Channels * w.Bits / 8
	fmtBody := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtBody[0:], 1)
	binary.LittleEndian.PutUint16(fmtBody[2:], uint16(w.Channels))
	binary.LittleEndian.PutUint32(fmtBody[4:], w.Rate)
	binary.LittleEndian.PutUint32(fmtBody[8:], w.Rate*uint32(ba))
	binary.LittleEndian.PutUint16(fmtBody[12:], uint16(ba))
	binary.LittleEndian.PutUint16(fmtBody[14:], uint16(w.Bits))
	if w.Extensible {
		binary.LittleEndian.PutUint16(fmtBody[0:], 0xFFFE)
		ext := make([]byte, 24)
		binary.LittleEndian.PutUint16(ext[0:], 22)
		binary.LittleEndian.PutUint16(ext[2:], uint16(w.Bits))
		binary.LittleEndian.PutUint16(ext[8:], 1) // KSDATAFORMAT_SUBTYPE_PCM starts with 1
		fmtBody = append(fmtBody, ext...)
	}
	body := []byte("WAVE")
	body = append(body, chunk("fmt ", fmtBody)...)
	if len(w.Info) > 0 {
		info := []byte("INFO")
		for id, v := range w.Info {
			info = append(info, chunk(id, append([]byte(v), 0))...)
		}
		body = append(body, chunk("LIST", info)...)
	}
	var data []byte
	for _, s := range w.Samples {
		switch w.Bits {
		case 8:
			data = append(data, byte(s+128))
		case 16:
			data = append(data, byte(s), byte(s>>8))
		case 24:
			data = append(data, byte(s), byte(s>>8), byte(s>>16))
		default:
			data = append(data, byte(s), byte(s>>8), byte(s>>16), byte(s>>24))
		}
	}
	body = append(body, chunk("data", data)...)
	return chunk("RIFF", body)
}

// WriteWAV writes the file to path.
func WriteWAV(t *testing.T, path string, w WAV) {
	t.Helper()
	if err := os.WriteFile(path, w.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
