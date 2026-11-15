package services

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

// DSDFile is where the DSD audio of a .dsf or .dff file lies and how it is laid out.
type DSDFile struct {
	Format   string `json:"f"`  // "DSF" or "DFF"
	Rate     uint32 `json:"r"`  // DSD samples per second per channel (2822400 for DSD64)
	Channels int    `json:"c"`  //
	Samples  uint64 `json:"n"`  // DSD samples (bits) per channel
	Data     int64  `json:"d"`  // where the audio starts in the file
	Block    int    `json:"b"`  // DSF: bytes of one channel per block; DFF: 0, bytes interleave
	LSBFirst bool   `json:"l"`  // DSF with 1 bit per sample: the first sample is the lowest bit
	Tag      int64  `json:"t"`  // where an ID3v2 tag starts, 0 when there is none
	TagSize  int64  `json:"ts"` // its length in bytes
}

// DSDKind names the DSD rate as people do: DSD64 for 2.8224 MHz.
func (f DSDFile) DSDKind() string {
	return fmt.Sprintf("DSD%d", f.Rate/44100)
}

// ByteAt is where channel ch's byte number i (from the start of the audio) lies in the file.
func (f DSDFile) ByteAt(ch int, i int64) int64 {
	if f.Block == 0 {
		return f.Data + i*int64(f.Channels) + int64(ch)
	}
	b := int64(f.Block)
	return f.Data + (i/b*int64(f.Channels)+int64(ch))*b + i%b
}

// ErrNotDSD is a file that is not plain DSD audio this player plays (DST compression, more than
// two channels, a broken header).
var ErrNotDSD = errors.New("not a playable DSD file")

// ParseDSF reads the header of a Sony DSF file: its first 92 bytes and the file's size.
func ParseDSF(head []byte, size int64) (DSDFile, error) {
	le := binary.LittleEndian
	if len(head) < 92 || string(head[0:4]) != "DSD " || string(head[28:32]) != "fmt " {
		return DSDFile{}, fmt.Errorf("%w: no DSF header", ErrNotDSD)
	}
	fmtSize := int64(le.Uint64(head[32:]))
	f := DSDFile{
		Format:   "DSF",
		Channels: int(le.Uint32(head[52:])),
		Rate:     le.Uint32(head[56:]),
		Samples:  le.Uint64(head[64:]),
		Block:    int(le.Uint32(head[72:])),
		LSBFirst: le.Uint32(head[60:]) == 1,
		Data:     28 + fmtSize + 12,
	}
	if le.Uint32(head[44:]) != 0 { // the format id: 0 is raw DSD
		return DSDFile{}, fmt.Errorf("%w: DSF format %d", ErrNotDSD, le.Uint32(head[44:]))
	}
	if meta := int64(le.Uint64(head[20:])); meta > 0 && meta < size {
		f.Tag, f.TagSize = meta, size-meta
	}
	return f, f.check()
}

// ParseDFF reads the chunks of a Philips DSDIFF file. The audio must be plain DSD (not DST).
func ParseDFF(r io.ReaderAt, size int64) (DSDFile, error) {
	be := binary.BigEndian
	h := make([]byte, 16)
	if _, err := r.ReadAt(h, 0); err != nil || string(h[0:4]) != "FRM8" || string(h[12:16]) != "DSD " {
		return DSDFile{}, fmt.Errorf("%w: no DSDIFF header", ErrNotDSD)
	}
	f := DSDFile{Format: "DFF"}
	end := min(size, 12+int64(be.Uint64(h[4:])))
	var walk func(from, to int64, inProp bool) error
	walk = func(from, to int64, inProp bool) error {
		for pos := from; pos+12 <= to; {
			ch := make([]byte, 12)
			if _, err := r.ReadAt(ch, pos); err != nil {
				return err
			}
			id, n, body := string(ch[0:4]), int64(be.Uint64(ch[4:])), pos+12
			if n < 0 || body+n > size {
				n = size - body
			}
			switch {
			case id == "PROP" && !inProp:
				if err := walk(body+4, body+n, true); err != nil { // after the "SND " type
					return err
				}
			case id == "FS  " && inProp:
				b := make([]byte, 4)
				if _, err := r.ReadAt(b, body); err != nil {
					return err
				}
				f.Rate = be.Uint32(b)
			case id == "CHNL" && inProp:
				b := make([]byte, 2)
				if _, err := r.ReadAt(b, body); err != nil {
					return err
				}
				f.Channels = int(be.Uint16(b))
			case id == "CMPR" && inProp:
				b := make([]byte, 4)
				if _, err := r.ReadAt(b, body); err != nil {
					return err
				}
				if string(b) != "DSD " {
					return fmt.Errorf("%w: compressed as %q", ErrNotDSD, strings.TrimSpace(string(b)))
				}
			case id == "DSD " && !inProp:
				f.Data = body
				if f.Channels > 0 {
					f.Samples = uint64(n) * 8 / uint64(f.Channels)
				}
			case id == "DST " && !inProp:
				return fmt.Errorf("%w: DST compressed", ErrNotDSD)
			case id == "ID3 " && !inProp:
				f.Tag, f.TagSize = body, n
			}
			pos = body + n + n%2 // chunks are padded to an even length
		}
		return nil
	}
	if err := walk(16, end, false); err != nil {
		return DSDFile{}, err
	}
	if f.Data == 0 {
		return DSDFile{}, fmt.Errorf("%w: no DSD chunk", ErrNotDSD)
	}
	return f, f.check()
}

func (f DSDFile) check() error {
	switch {
	case f.Channels < 1 || f.Channels > 2:
		return fmt.Errorf("%w: %d channels (stereo and mono only)", ErrNotDSD, f.Channels)
	case f.Rate < 2822400 || f.Rate%44100 != 0 && f.Rate%48000 != 0:
		return fmt.Errorf("%w: rate %d", ErrNotDSD, f.Rate)
	case f.Samples == 0:
		return fmt.Errorf("%w: no audio", ErrNotDSD)
	}
	return nil
}

// ID3 is what the player takes from an ID3v2 tag: the tags under the names FLAC uses
// (TITLE, ARTIST, ...) and the front cover.
type ID3 struct {
	Tags        map[string]string
	HasPicture  bool
	Picture     []byte // with withPicture only
	PictureMIME string
}

var id3Names = map[string]string{
	"TIT2": "TITLE", "TPE1": "ARTIST", "TPE2": "ALBUMARTIST", "TALB": "ALBUM",
	"TDRC": "DATE", "TYER": "DATE", "TCON": "GENRE", "TRCK": "TRACKNUMBER", "TPOS": "DISCNUMBER",
}

// ParseID3 reads an ID3v2.3 or v2.4 tag (DSF and DFF carry one). withPicture: keep the
// picture's bytes too (the cover), not only whether there is one.
func ParseID3(tag []byte, withPicture bool) (ID3, bool) {
	out := ID3{Tags: map[string]string{}}
	if len(tag) < 10 || string(tag[0:3]) != "ID3" {
		return out, false
	}
	ver, flags := tag[3], tag[5]
	if ver != 3 && ver != 4 {
		return out, false
	}
	size := int(syncsafe(tag[6:10]))
	body := tag[10:min(len(tag), 10+size)]
	if flags&0x80 != 0 && ver == 3 { // unsynchronised as a whole
		body = unsync(body)
	}
	if flags&0x40 != 0 && len(body) >= 4 { // an extended header
		n := int(binary.BigEndian.Uint32(body))
		if ver == 4 {
			n = int(syncsafe(body[:4]))
		} else {
			n += 4
		}
		body = body[min(n, len(body)):]
	}
	front := false
	for len(body) >= 10 && body[0] != 0 {
		id := string(body[0:4])
		n := int(binary.BigEndian.Uint32(body[4:8]))
		if ver == 4 {
			n = int(syncsafe(body[4:8]))
		}
		if n < 0 || 10+n > len(body) {
			break
		}
		data := body[10 : 10+n]
		if ver == 4 && body[9]&0x02 != 0 { // this frame alone unsynchronised
			data = unsync(data)
		}
		body = body[10+n:]
		switch {
		case id == "APIC":
			mime, pic, ok := apic(data)
			if ok && (!out.HasPicture || pic.front && !front) {
				out.HasPicture, front = true, pic.front
				if withPicture {
					out.Picture, out.PictureMIME = pic.data, mime
				}
			}
		case id == "COMM":
			if _, ok := out.Tags["COMMENT"]; !ok && len(data) > 4 {
				// encoding, a language of 3 letters, a description, the text
				parts := splitText(data[0], data[4:])
				if len(parts) >= 2 {
					out.Tags["COMMENT"] = parts[1]
				}
			}
		case id3Names[id] != "":
			if _, ok := out.Tags[id3Names[id]]; !ok && len(data) > 1 {
				if s := strings.TrimSpace(strings.Join(splitText(data[0], data[1:]), "; ")); s != "" {
					out.Tags[id3Names[id]] = s
				}
			}
		}
	}
	return out, true
}

type picture struct {
	data  []byte
	front bool
}

// apic reads a picture frame: encoding, MIME, type, description, the image.
func apic(d []byte) (string, picture, bool) {
	if len(d) < 4 {
		return "", picture{}, false
	}
	enc := d[0]
	z := strings.IndexByte(string(d[1:]), 0)
	if z < 0 || 1+z+2 > len(d) {
		return "", picture{}, false
	}
	mime := string(d[1 : 1+z])
	kind := d[1+z+1]
	rest := d[1+z+2:]
	_, rest = cutText(enc, rest) // the description
	if mime == "" || !strings.Contains(mime, "/") {
		mime = "image/" + strings.ToLower(strings.TrimPrefix(mime, "image/"))
	}
	return mime, picture{data: rest, front: kind == 3}, len(rest) > 0
}

// splitText decodes the strings of a text frame (v2.4 may hold several, zero-separated).
func splitText(enc byte, d []byte) []string {
	var out []string
	for len(d) > 0 {
		var s string
		s, d = cutText(enc, d)
		out = append(out, s)
	}
	return out
}

// cutText takes one zero-terminated string in the frame's encoding off the front of d.
func cutText(enc byte, d []byte) (string, []byte) {
	if enc == 1 || enc == 2 { // UTF-16 with a BOM, UTF-16 big-endian
		end := len(d) - len(d)%2
		for i := 0; i+1 < len(d); i += 2 {
			if d[i] == 0 && d[i+1] == 0 {
				end = i
				break
			}
		}
		s := decodeUTF16(d[:end], enc == 2)
		return s, d[min(len(d), end+2):]
	}
	end := len(d)
	if i := strings.IndexByte(string(d), 0); i >= 0 {
		end = i
	}
	s := d[:end]
	rest := d[min(len(d), end+1):]
	if enc == 3 {
		return string(s), rest
	}
	r := make([]rune, len(s)) // ISO-8859-1
	for i, b := range s {
		r[i] = rune(b)
	}
	return string(r), rest
}

func decodeUTF16(b []byte, bigEndian bool) string {
	if len(b) >= 2 && !bigEndian {
		switch {
		case b[0] == 0xFF && b[1] == 0xFE:
			b = b[2:]
		case b[0] == 0xFE && b[1] == 0xFF:
			b, bigEndian = b[2:], true
		}
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = binary.BigEndian.Uint16(b[2*i:])
		} else {
			u[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
	}
	return string(utf16.Decode(u))
}

func syncsafe(b []byte) uint32 {
	return uint32(b[0]&0x7f)<<21 | uint32(b[1]&0x7f)<<14 | uint32(b[2]&0x7f)<<7 | uint32(b[3]&0x7f)
}

// unsync drops the zero byte ID3 inserts after every 0xFF.
func unsync(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xFF && i+1 < len(b) && b[i+1] == 0 {
			i++
		}
	}
	return out
}

// ReverseBits turns a byte with its first sample in the lowest bit around (DSF stores 1-bit
// samples that way; the converter takes the first sample in the highest bit).
var ReverseBits = func() (t [256]byte) {
	for v := range 256 {
		var r byte
		for b := range 8 {
			if v&(1<<b) != 0 {
				r |= 0x80 >> b
			}
		}
		t[v] = r
	}
	return
}()
