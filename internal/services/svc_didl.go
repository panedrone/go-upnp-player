package services

import (
	"fmt"
	"math"
	"strings"
)

// TrackMeta is what the renderer shows on its screen.
type TrackMeta struct {
	Title       string
	Artist      string
	Album       string
	Date        string
	Genre       string
	TrackNumber int
	Duration    float64 // seconds
	SampleRate  uint32
	Bits        uint8
	Channels    uint8
	Size        int64
	Mime        string
	URL         string
	ArtURL      string
}

// DLNAFeatures is contentFeatures for the HTTP header and protocolInfo:
// OP=01 means byte seeking (Range) is supported.
const DLNAFeatures = "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000"

// ProtocolInfo for the res element.
func ProtocolInfo(mime string) string {
	return "http-get:*:" + mime + ":" + DLNAFeatures
}

// FormatHMS formats H:MM:SS.mmm, the UPnP duration format.
func FormatHMS(sec float64) string {
	if sec < 0 || math.IsNaN(sec) {
		sec = 0
	}
	ms := int64(math.Round(sec * 1000))
	h := ms / 3600000
	m := ms / 60000 % 60
	s := ms / 1000 % 60
	return fmt.Sprintf("%d:%02d:%02d.%03d", h, m, s, ms%1000)
}

// FormatSeekTarget formats H:MM:SS for AVTransport.Seek (not all renderers understand fractions).
func FormatSeekTarget(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	t := int64(sec)
	return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
}

// ParseHMS parses H:MM:SS[.fff]; "NOT_IMPLEMENTED" and garbage yield 0.
func ParseHMS(s string) float64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0
	}
	var h, m int
	var sec float64
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return 0
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return 0
	}
	if _, err := fmt.Sscanf(parts[2], "%g", &sec); err != nil {
		return 0
	}
	return float64(h*3600+m*60) + sec
}

// BuildDIDL builds DIDL-Lite for CurrentURIMetaData/NextURIMetaData.
// Without metadata some renderers refuse to play.
func BuildDIDL(m TrackMeta) string {
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/">`)
	b.WriteString(`<item id="1" parentID="0" restricted="1">`)
	writeTag(&b, "dc:title", m.Title)
	writeTag(&b, "dc:creator", m.Artist)
	writeTag(&b, "upnp:artist", m.Artist)
	writeTag(&b, "upnp:album", m.Album)
	writeTag(&b, "dc:date", m.Date)
	writeTag(&b, "upnp:genre", m.Genre)
	if m.TrackNumber > 0 {
		writeTag(&b, "upnp:originalTrackNumber", fmt.Sprint(m.TrackNumber))
	}
	writeTag(&b, "upnp:albumArtURI", m.ArtURL)
	b.WriteString(`<upnp:class>object.item.audioItem.musicTrack</upnp:class>`)
	b.WriteString(`<res protocolInfo="`)
	b.WriteString(EscapeXML(ProtocolInfo(m.Mime)))
	b.WriteString(`" duration="`)
	b.WriteString(FormatHMS(m.Duration))
	b.WriteString(`"`)
	if m.Size > 0 {
		fmt.Fprintf(&b, ` size="%d"`, m.Size)
	}
	if m.SampleRate > 0 {
		fmt.Fprintf(&b, ` sampleFrequency="%d"`, m.SampleRate)
	}
	if m.Bits > 0 {
		fmt.Fprintf(&b, ` bitsPerSample="%d"`, m.Bits)
	}
	if m.Channels > 0 {
		fmt.Fprintf(&b, ` nrAudioChannels="%d"`, m.Channels)
	}
	b.WriteString(`>`)
	b.WriteString(EscapeXML(m.URL))
	b.WriteString(`</res></item></DIDL-Lite>`)
	return b.String()
}

func writeTag(b *strings.Builder, tag, value string) {
	if value == "" {
		return
	}
	b.WriteString("<" + tag + ">")
	b.WriteString(EscapeXML(value))
	b.WriteString("</" + tag + ">")
}

var xmlEscaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
)

// EscapeXML escapes text for insertion into XML (including attributes).
func EscapeXML(s string) string {
	return xmlEscaper.Replace(s)
}
