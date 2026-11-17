package services

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// WAVFile is where a WAV file keeps its PCM and its tags.
type WAVFile struct {
	Rate     uint32 `json:"r"`
	Channels int    `json:"c"`
	Bits     int    `json:"b"` // bits of one sample in the file: 8, 16, 24 or 32
	Data     int64  `json:"d"` // where the PCM starts in the file
	Frames   uint64 `json:"n"` // samples per channel
	Tag      int64  `json:"t"` // where an "id3 " chunk's ID3v2 tag starts, 0 when there is none
	TagSize  int64  `json:"ts"`
}

// BlockAlign is the bytes of one sample of every channel.
func (f WAVFile) BlockAlign() int64 { return int64(f.Channels) * int64(f.Bits/8) }

// infoNames maps the RIFF LIST INFO fields to the tag names the library uses.
var infoNames = map[string]string{
	"INAM": "TITLE", "IART": "ARTIST", "IPRD": "ALBUM", "ICRD": "DATE", "IGNR": "GENRE",
	"ITRK": "TRACKNUMBER", "IPRT": "TRACKNUMBER", "ICMT": "COMMENT",
}

// ParseWAV reads the chunks of a RIFF/WAVE file of size bytes: the format, where the PCM is,
// the LIST INFO tags and where an ID3 tag is. Only integer PCM plays (bit-perfect as it is);
// float and compressed WAV are refused.
func ParseWAV(r io.ReaderAt, size int64) (WAVFile, map[string]string, error) {
	var f WAVFile
	tags := map[string]string{}
	head := make([]byte, 12)
	if _, err := r.ReadAt(head, 0); err != nil {
		return f, nil, fmt.Errorf("wav: %w", err)
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return f, nil, errors.New("wav: not a RIFF/WAVE file")
	}
	var haveFmt bool
	var dataLen int64
	ch := make([]byte, 8)
	for at := int64(12); at+8 <= size; {
		if _, err := r.ReadAt(ch, at); err != nil {
			break
		}
		id, n := string(ch[0:4]), int64(binary.LittleEndian.Uint32(ch[4:8]))
		body := at + 8
		switch id {
		case "fmt ":
			b := make([]byte, min(n, 40))
			if n < 16 {
				return f, nil, errors.New("wav: short fmt chunk")
			}
			if _, err := r.ReadAt(b, body); err != nil {
				return f, nil, fmt.Errorf("wav: %w", err)
			}
			format := binary.LittleEndian.Uint16(b[0:2])
			if format == 0xFFFE && len(b) >= 26 { // WAVE_FORMAT_EXTENSIBLE: the subformat's first two bytes
				format = binary.LittleEndian.Uint16(b[24:26])
			}
			if format != 1 {
				return f, nil, fmt.Errorf("wav: format %d is not integer PCM", format)
			}
			f.Channels = int(binary.LittleEndian.Uint16(b[2:4]))
			f.Rate = binary.LittleEndian.Uint32(b[4:8])
			f.Bits = int(binary.LittleEndian.Uint16(b[14:16]))
			haveFmt = true
		case "data":
			f.Data, dataLen = body, n
			if n == 0xFFFFFFFF || body+n > size { // written while streaming, or cut short
				dataLen = size - body
			}
		case "LIST":
			readInfo(r, body, n, tags)
		case "id3 ", "ID3 ":
			f.Tag, f.TagSize = body, n
		}
		at = body + n + n%2 // chunks are padded to an even length
	}
	switch {
	case !haveFmt:
		return f, nil, errors.New("wav: no fmt chunk")
	case f.Data == 0:
		return f, nil, errors.New("wav: no data chunk")
	case f.Channels < 1 || f.Rate == 0:
		return f, nil, errors.New("wav: bad format")
	case f.Bits != 8 && f.Bits != 16 && f.Bits != 24 && f.Bits != 32:
		return f, nil, fmt.Errorf("wav: %d bits per sample", f.Bits)
	}
	f.Frames = uint64(dataLen / f.BlockAlign())
	return f, tags, nil
}

// readInfo reads a LIST chunk of type INFO into tags (other LIST types are skipped).
func readInfo(r io.ReaderAt, at, n int64, tags map[string]string) {
	if n < 4 || n > 1<<20 {
		return
	}
	b := make([]byte, n)
	if _, err := r.ReadAt(b, at); err != nil || string(b[0:4]) != "INFO" {
		return
	}
	for p := 4; p+8 <= len(b); {
		id, sz := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:p+8]))
		if sz < 0 || p+8+sz > len(b) {
			return
		}
		v := strings.TrimSpace(strings.TrimRight(string(b[p+8:p+8+sz]), "\x00"))
		if name, ok := infoNames[id]; ok && v != "" {
			if _, have := tags[name]; !have {
				tags[name] = v
			}
		}
		p += 8 + sz + sz%2
	}
}

// WAVSamples turns PCM bytes of the file (whole samples of every channel) into samples per
// channel. 8-bit WAV is unsigned: it is made signed, as the stream's WAV writer expects.
func (f WAVFile) WAVSamples(b []byte) [][]int32 {
	ba := int(f.BlockAlign())
	n := len(b) / ba
	bytes := f.Bits / 8
	out := make([][]int32, f.Channels)
	for ch := range out {
		out[ch] = make([]int32, n)
	}
	for i := 0; i < n; i++ {
		for ch := 0; ch < f.Channels; ch++ {
			p := b[i*ba+ch*bytes:]
			var v int32
			switch bytes {
			case 1:
				v = int32(p[0]) - 128
			case 2:
				v = int32(int16(binary.LittleEndian.Uint16(p)))
			case 3:
				v = int32(uint32(p[0])|uint32(p[1])<<8|uint32(p[2])<<16) << 8 >> 8
			default:
				v = int32(binary.LittleEndian.Uint32(p))
			}
			out[ch][i] = v
		}
	}
	return out
}
