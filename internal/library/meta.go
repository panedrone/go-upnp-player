package library

import (
	"fmt"
	"os"
	"strings"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
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
}

// readFlacMeta parses metadata; flac.ParseFile reads only the metadata
// blocks and does not touch the audio.
var readFlacMeta = func(path string, st os.FileInfo) (*FileMeta, error) {
	s, err := flac.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("flac %s: %w", path, err)
	}
	defer func() { _ = s.Close() }()
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
	s, err := flac.ParseFile(path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = s.Close() }()
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
