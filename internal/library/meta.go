package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"

	"github.com/panedrone/go-upnp-player/internal/services"
)

// FileMeta is what we read from the FLAC header. Cached by path+mtime+size.
type FileMeta struct {
	ModTime    int64             `json:"m"`
	Size       int64             `json:"s"`
	SampleRate uint32            `json:"sr"`
	Channels   uint8             `json:"ch"`
	Bits       uint8             `json:"b"`
	NSamples   uint64            `json:"n"`
	Seekable   bool              `json:"sk"`
	HasPicture bool              `json:"p,omitempty"`
	Tags       map[string]string `json:"t,omitempty"`
	// DSD: a .dsf or .dff file; SampleRate is then the DSD rate and NSamples its 1-bit samples
	DSD *services.DSDFile `json:"dsd,omitempty"`
	// WAV: a .wav file
	WAV *services.WAVFile `json:"wav,omitempty"`
}

// parseBlocks reads only the metadata blocks and does not touch the audio. The
// file is opened here because flac.ParseFile leaks it: Stream.Close does nothing
// (the stream holds a bufio.Reader, not the file), so every scanned file stayed
// open, and locked on Windows, until the garbage collector got to it (2026-11-09).
func parseBlocks(path string) (*flac.Stream, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return flac.Parse(f)
}

// readFlacMeta parses the metadata of a FLAC file.
var readFlacMeta = func(path string, st os.FileInfo) (*FileMeta, error) {
	s, err := parseBlocks(path)
	if err != nil {
		return nil, fmt.Errorf("flac %s: %w", path, err)
	}
	m := &FileMeta{
		ModTime:    st.ModTime().UnixNano(),
		Size:       st.Size(),
		SampleRate: s.Info.SampleRate,
		Channels:   s.Info.NChannels,
		Bits:       s.Info.BitsPerSample,
		NSamples:   s.Info.NSamples,
		Tags:       map[string]string{},
	}
	for _, b := range s.Blocks {
		switch body := b.Body.(type) {
		case *meta.SeekTable:
			m.Seekable = len(body.Points) > 0
		case *meta.VorbisComment:
			for _, kv := range body.Tags {
				k := strings.ToUpper(kv[0])
				if _, ok := m.Tags[k]; !ok {
					m.Tags[k] = kv[1]
				}
			}
		case *meta.Picture:
			m.HasPicture = true
		}
	}
	return m, nil
}

// readEmbeddedPicture returns the cover from a PICTURE block (preferably "Cover (front)").
var readEmbeddedPicture = func(path string) (data []byte, mime string, err error) {
	s, err := parseBlocks(path)
	if err != nil {
		return nil, "", err
	}
	var best *meta.Picture
	for _, b := range s.Blocks {
		if p, ok := b.Body.(*meta.Picture); ok && p.MIME != "-->" {
			if best == nil || p.Type == 3 {
				best = p
			}
		}
	}
	if best == nil {
		return nil, "", os.ErrNotExist
	}
	return best.Data, best.MIME, nil
}

// isDSD reports a DSD file by its extension: Sony's DSF and Philips' DSDIFF.
func isDSD(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".dsf", ".dff":
		return true
	}
	return false
}

// isWAV reports a WAV file by its extension.
func isWAV(path string) bool { return strings.EqualFold(filepath.Ext(path), ".wav") }

// readWAVMeta reads the chunks of a WAV file (not the audio): its LIST INFO tags, and an ID3
// tag over them when the file has one.
var readWAVMeta = func(path string, st os.FileInfo) (*FileMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	w, tags, err := services.ParseWAV(f, st.Size())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m := &FileMeta{
		ModTime: st.ModTime().UnixNano(), Size: st.Size(),
		SampleRate: w.Rate, Channels: uint8(w.Channels), Bits: uint8(w.Bits), NSamples: w.Frames,
		Seekable: true, Tags: tags, WAV: &w,
	}
	if tag, err := readTagAt(f, w.Tag, w.TagSize); err == nil {
		if t, ok := services.ParseID3(tag, false); ok {
			for k, v := range t.Tags {
				m.Tags[k] = v
			}
			m.HasPicture = t.HasPicture
		}
	}
	return m, nil
}

// maxTag caps what is read of an ID3 tag (a cover is part of it).
const maxTag = 32 << 20

// readDSDMeta reads the header and the ID3 tag of a DSF or DFF file (not the audio).
var readDSDMeta = func(path string, st os.FileInfo) (*FileMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var d services.DSDFile
	if strings.EqualFold(filepath.Ext(path), ".dsf") {
		head := make([]byte, 92)
		if _, err := f.ReadAt(head, 0); err != nil {
			return nil, fmt.Errorf("dsf %s: %w", path, err)
		}
		d, err = services.ParseDSF(head, st.Size())
	} else {
		d, err = services.ParseDFF(f, st.Size())
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m := &FileMeta{
		ModTime: st.ModTime().UnixNano(), Size: st.Size(),
		SampleRate: d.Rate, Channels: uint8(d.Channels), Bits: 1, NSamples: d.Samples,
		Seekable: true, Tags: map[string]string{}, DSD: &d,
	}
	if tag, err := readTagAt(f, d.Tag, d.TagSize); err == nil {
		if t, ok := services.ParseID3(tag, false); ok {
			m.Tags, m.HasPicture = t.Tags, t.HasPicture
		}
	}
	return m, nil
}

// readTagAt reads the ID3 tag of size bytes at offset at of a DSD or WAV file.
func readTagAt(f *os.File, at, size int64) ([]byte, error) {
	if at == 0 || size <= 0 {
		return nil, os.ErrNotExist
	}
	tag := make([]byte, min(size, maxTag))
	n, err := f.ReadAt(tag, at)
	if n == 0 {
		return nil, err
	}
	return tag[:n], nil
}

// readID3Picture returns the cover from the ID3 tag of a DSD or WAV file.
var readID3Picture = func(path string, at, size int64) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()
	tag, err := readTagAt(f, at, size)
	if err != nil {
		return nil, "", err
	}
	t, ok := services.ParseID3(tag, true)
	if !ok || t.Picture == nil {
		return nil, "", os.ErrNotExist
	}
	return t.Picture, t.PictureMIME, nil
}

// readID3Comment returns the comment of a DSD or WAV file's ID3 tag, for the properties panel.
func readID3Comment(path string, at, size int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	tag, err := readTagAt(f, at, size)
	if err != nil {
		return ""
	}
	t, _ := services.ParseID3(tag, false)
	return t.Tags["COMMENT"]
}
