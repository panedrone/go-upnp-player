package library

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/mewkiz/flac/meta"

	"github.com/panedrone/go-upnp-player/internal/services"
)

// FileDetails is what the properties panel shows beyond the indexed track: read
// from the FLAC header on request, not kept in the index or in the meta cache.
type FileDetails struct {
	Vendor      string
	MD5         string // hex, upper case; "" when the encoder left it zero
	EmbeddedCue bool
	Comment     string
}

// readFileDetails reads the metadata blocks of a FLAC file again (the audio is not touched).
var readFileDetails = func(path string) (*FileDetails, error) {
	s, err := parseBlocks(path)
	if err != nil {
		return nil, fmt.Errorf("flac %s: %w", path, err)
	}
	d := &FileDetails{}
	if s.Info.MD5sum != [16]byte{} {
		d.MD5 = strings.ToUpper(hex.EncodeToString(s.Info.MD5sum[:]))
	}
	for _, b := range s.Blocks {
		switch body := b.Body.(type) {
		case *meta.VorbisComment:
			d.Vendor = body.Vendor
			for _, kv := range body.Tags {
				switch strings.ToUpper(kv[0]) {
				case "CUESHEET":
					d.EmbeddedCue = true
				case "COMMENT", "DESCRIPTION":
					if d.Comment == "" {
						d.Comment = kv[1]
					}
				}
			}
		case *meta.CueSheet:
			d.EmbeddedCue = true
		}
	}
	return d, nil
}

// Properties is a track as the properties panel shows it.
type Properties struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Date        string  `json:"date"`
	Genre       string  `json:"genre"`
	Comment     string  `json:"comment"`
	Duration    float64 `json:"duration"`
	Samples     uint64  `json:"samples"`
	SampleRate  uint32  `json:"sampleRate"`
	Channels    uint8   `json:"channels"`
	Bits        uint8   `json:"bits"`
	BitrateKbps int     `json:"bitrateKbps"`
	Codec       string  `json:"codec"`
	Encoding    string  `json:"encoding"`
	Tool        string  `json:"tool"`
	EmbeddedCue bool    `json:"embeddedCue"`
	MD5         string  `json:"md5"`
}

// PropertiesOf joins the indexed track with the details read from its file.
func PropertiesOf(t *Track) (*Properties, error) {
	d, err := readFileDetails(t.Path)
	if err != nil {
		return nil, err
	}
	return &Properties{
		Number: t.Number, Title: t.Title, Artist: t.Artist, Album: t.Album, Date: t.Date, Genre: t.Genre,
		Comment: d.Comment, Duration: t.Duration, Samples: t.End - t.Start,
		SampleRate: t.SampleRate, Channels: t.Channels, Bits: t.Bits,
		// a cue track has no size of its own: the file's average, as foobar2000 shows it
		BitrateKbps: services.AvgBitrateKbps(t.FileSize, t.FileFrames, t.SampleRate),
		Codec:       "FLAC", Encoding: "lossless", Tool: d.Vendor, EmbeddedCue: d.EmbeddedCue, MD5: d.MD5,
	}, nil
}
