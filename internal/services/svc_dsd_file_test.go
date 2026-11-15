package services

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
)

// the first 92 bytes of a real DSD128 stereo DSF (Boris Blank, Resonance)
const dsfHead = "445344201c00000000000000d162e312000000005c60e31200000000666d7420340000000000000001000000" +
	"0000000002000000020000000022560001000000801f8d4b000000000010000000000000646174610c60e31200000000"

func TestParseDSF(t *testing.T) {
	head, _ := hex.DecodeString(dsfHead)
	f, err := ParseDSF(head, 316891857)
	if err != nil {
		t.Fatal(err)
	}
	want := DSDFile{Format: "DSF", Rate: 5644800, Channels: 2, Samples: 0x4b8d1f80, Data: 92,
		Block: 4096, LSBFirst: true, Tag: 316891228, TagSize: 316891857 - 316891228}
	if f != want {
		t.Fatalf("got %+v\nwant %+v", f, want)
	}
	if f.DSDKind() != "DSD128" {
		t.Errorf("kind %s", f.DSDKind())
	}
	// block layout: channel 1's byte 4097 is in the second block group, after channel 0's block
	if got := f.ByteAt(1, 4097); got != 92+(1*2+1)*4096+1 {
		t.Errorf("ByteAt %d", got)
	}
	if _, err := ParseDSF(head[:50], 1000); !errors.Is(err, ErrNotDSD) {
		t.Errorf("short header: %v", err)
	}
}

// dffChunk is an IFF chunk: id, a 64-bit big-endian size, the body, padded to even.
func dffChunk(id string, body []byte) []byte {
	b := append([]byte(id), make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[4:], uint64(len(body)))
	b = append(b, body...)
	if len(body)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

func dffFile(cmpr string, channels int, audio []byte, extra ...[]byte) []byte {
	fs := make([]byte, 4)
	binary.BigEndian.PutUint32(fs, 2822400)
	chnl := make([]byte, 2+4*channels)
	binary.BigEndian.PutUint16(chnl, uint16(channels))
	prop := append([]byte("SND "), dffChunk("FS  ", fs)...)
	prop = append(prop, dffChunk("CHNL", chnl)...)
	prop = append(prop, dffChunk("CMPR", append([]byte(cmpr), 0, 0))...)
	body := append([]byte("DSD "), dffChunk("FVER", []byte{1, 5, 0, 0})...)
	body = append(body, dffChunk("PROP", prop)...)
	body = append(body, dffChunk("DSD ", audio)...)
	for _, e := range extra {
		body = append(body, e...)
	}
	return dffChunk("FRM8", body)
}

func TestParseDFF(t *testing.T) {
	tag := id3Tag(3, id3Frame(3, "TIT2", append([]byte{3}, "Song"...)))
	data := dffFile("DSD ", 2, make([]byte, 1000), dffChunk("ID3 ", tag))
	f, err := ParseDFF(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Format != "DFF" || f.Rate != 2822400 || f.Channels != 2 || f.Samples != 4000 || f.Block != 0 || f.LSBFirst {
		t.Fatalf("%+v", f)
	}
	if string(data[f.Data-12:f.Data-8]) != "DSD " {
		t.Errorf("data at %d", f.Data)
	}
	if got := f.ByteAt(1, 10); got != f.Data+21 {
		t.Errorf("ByteAt %d", got)
	}
	if f.Tag == 0 || string(data[f.Tag:f.Tag+3]) != "ID3" || f.TagSize != int64(len(tag)) {
		t.Errorf("tag at %d size %d", f.Tag, f.TagSize)
	}
	dst := dffFile("DST ", 2, make([]byte, 10))
	if _, err := ParseDFF(bytes.NewReader(dst), int64(len(dst))); !errors.Is(err, ErrNotDSD) {
		t.Errorf("DST: %v", err)
	}
	six := dffFile("DSD ", 6, make([]byte, 60))
	if _, err := ParseDFF(bytes.NewReader(six), int64(len(six))); !errors.Is(err, ErrNotDSD) {
		t.Errorf("6 channels: %v", err)
	}
}

func syncsafeBytes(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

func id3Frame(ver byte, id string, data []byte) []byte {
	b := []byte(id)
	if ver == 4 {
		b = append(b, syncsafeBytes(len(data))...)
	} else {
		b = binary.BigEndian.AppendUint32(b, uint32(len(data)))
	}
	return append(append(b, 0, 0), data...)
}

func id3Tag(ver byte, frames ...[]byte) []byte {
	var body []byte
	for _, f := range frames {
		body = append(body, f...)
	}
	return append(append([]byte{'I', 'D', '3', ver, 0, 0}, syncsafeBytes(len(body))...), body...)
}

func TestParseID3(t *testing.T) {
	utf16 := []byte{1, 0xFF, 0xFE, 'R', 0, 0xE9, 0, 's', 0} // "Rés", UTF-16 with a BOM
	pic := append([]byte{0}, "image/jpeg\x00"...)
	pic = append(append(pic, 3), "front\x00"...)
	pic = append(pic, 0xFF, 0xD8, 0xFF)
	back := append(append(append([]byte{0}, "image/png\x00"...), 4, 0), 1, 2)
	tag := id3Tag(3,
		id3Frame(3, "TALB", utf16),
		id3Frame(3, "TPE1", append([]byte{0}, "Yello\x00"...)),
		id3Frame(3, "TRCK", append([]byte{3}, "2/12"...)),
		id3Frame(3, "TYER", append([]byte{0}, "2020"...)),
		id3Frame(3, "COMM", append([]byte{0, 'e', 'n', 'g'}, "\x00a note"...)),
		id3Frame(3, "APIC", back),
		id3Frame(3, "APIC", pic),
	)
	got, ok := ParseID3(tag, true)
	if !ok {
		t.Fatal("not parsed")
	}
	for k, v := range map[string]string{"ALBUM": "Rés", "ARTIST": "Yello", "TRACKNUMBER": "2/12", "DATE": "2020", "COMMENT": "a note"} {
		if got.Tags[k] != v {
			t.Errorf("%s = %q, want %q", k, got.Tags[k], v)
		}
	}
	if !got.HasPicture || got.PictureMIME != "image/jpeg" || !bytes.Equal(got.Picture, []byte{0xFF, 0xD8, 0xFF}) {
		t.Errorf("picture %v %q %v: the front cover wins", got.HasPicture, got.PictureMIME, got.Picture)
	}
	if lean, _ := ParseID3(tag, false); !lean.HasPicture || lean.Picture != nil {
		t.Error("without withPicture: only whether there is one")
	}

	v4 := id3Tag(4, id3Frame(4, "TDRC", append([]byte{3}, "1986\x00"...)), id3Frame(4, "TCON", append([]byte{3}, "Rock\x00Metal"...)))
	got, _ = ParseID3(v4, false)
	if got.Tags["DATE"] != "1986" || got.Tags["GENRE"] != "Rock; Metal" {
		t.Errorf("v2.4: %v", got.Tags)
	}
	if _, ok := ParseID3([]byte("TAG...."), false); ok {
		t.Error("an ID3v1 tag taken for v2")
	}
}

func TestReverseBits(t *testing.T) {
	if ReverseBits[0x01] != 0x80 || ReverseBits[0x69] != 0x96 || ReverseBits[0xF0] != 0x0F {
		t.Errorf("%x %x %x", ReverseBits[0x01], ReverseBits[0x69], ReverseBits[0xF0])
	}
}
