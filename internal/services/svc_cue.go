package services

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

// CueSheet is a parsed .cue: album-wide fields and the list of FILEs with their tracks.
type CueSheet struct {
	Performer string
	Title     string
	Date      string
	Genre     string
	Files     []CueFile
}

// CueFile is one FILE directive and the tracks that live in that file.
type CueFile struct {
	Name   string
	Type   string
	Tracks []CueTrack
}

// CueTrack is TRACK nn AUDIO. Index00/Index01 are in CD frames (1/75 s), -1 if absent.
type CueTrack struct {
	Number    int
	Title     string
	Performer string
	Index00   int64
	Index01   int64
}

// CueSpan is a track laid out as a sample range inside the audio file [Start, End).
type CueSpan struct {
	Number int
	Start  uint64
	End    uint64
}

// DecodeText converts the bytes of a text file (cue, tags) to UTF-8.
// UTF-8 with or without BOM, UTF-16 with BOM; everything else is treated as cp1251:
// old rips with Russian titles are almost always in it.
func DecodeText(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return string(data[3:])
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		out, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(data)
		if err == nil {
			return string(out)
		}
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		out, err := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder().Bytes(data)
		if err == nil {
			return string(out)
		}
	}
	if utf8.Valid(data) {
		return string(data)
	}
	out, err := charmap.Windows1251.NewDecoder().Bytes(data)
	if err != nil {
		return string(data)
	}
	return string(out)
}

// ParseCue parses the text of a cue sheet. Unknown commands are skipped.
func ParseCue(data []byte) (*CueSheet, error) {
	text := DecodeText(data)
	sheet := &CueSheet{}
	var file *CueFile
	var track *CueTrack

	flushTrack := func() {
		if track != nil && file != nil {
			file.Tracks = append(file.Tracks, *track)
		}
		track = nil
	}
	flushFile := func() {
		flushTrack()
		if file != nil {
			sheet.Files = append(sheet.Files, *file)
		}
		file = nil
	}

	for lineNo, raw := range strings.Split(text, "\n") {
		args := splitCueLine(strings.TrimSpace(raw))
		if len(args) == 0 {
			continue
		}
		cmd := strings.ToUpper(args[0])
		switch cmd {
		case "FILE":
			if len(args) < 2 {
				return nil, fmt.Errorf("cue line %d: FILE without name", lineNo+1)
			}
			// EAC "tracks with gaps": the TRACK is declared at the end of the previous file
			// (only its INDEX 00, the pregap, is there), while INDEX 01 is already in the new FILE.
			// Such a track moves to the new file; the pregap stays as the tail of the previous one.
			var carry *CueTrack
			if track != nil && track.Index01 < 0 {
				carry, track = track, nil
				carry.Index00 = -1
			}
			flushFile()
			file = &CueFile{Name: args[1]}
			track = carry
			if len(args) > 2 {
				file.Type = strings.ToUpper(args[2])
			}
		case "TRACK":
			flushTrack()
			if file == nil {
				return nil, fmt.Errorf("cue line %d: TRACK before FILE", lineNo+1)
			}
			if len(args) < 2 {
				return nil, fmt.Errorf("cue line %d: TRACK without number", lineNo+1)
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, fmt.Errorf("cue line %d: bad track number %q", lineNo+1, args[1])
			}
			track = &CueTrack{Number: n, Index00: -1, Index01: -1}
		case "INDEX":
			if track == nil || len(args) < 3 {
				continue
			}
			frames, err := CueTimeToFrames(args[2])
			if err != nil {
				return nil, fmt.Errorf("cue line %d: %w", lineNo+1, err)
			}
			switch args[1] {
			case "00", "0":
				track.Index00 = frames
			case "01", "1":
				track.Index01 = frames
			}
		case "TITLE":
			if len(args) < 2 {
				continue
			}
			if track != nil {
				track.Title = args[1]
			} else {
				sheet.Title = args[1]
			}
		case "PERFORMER":
			if len(args) < 2 {
				continue
			}
			if track != nil {
				track.Performer = args[1]
			} else {
				sheet.Performer = args[1]
			}
		case "REM":
			if len(args) < 3 || track != nil {
				continue
			}
			value := strings.Join(args[2:], " ")
			switch strings.ToUpper(args[1]) {
			case "DATE":
				sheet.Date = value
			case "GENRE":
				sheet.Genre = value
			}
		}
	}
	flushFile()

	// a track without INDEX 01 (a fragment at the end of the sheet) is dropped, not the whole cue
	tracks := 0
	for fi := range sheet.Files {
		kept := sheet.Files[fi].Tracks[:0]
		for _, t := range sheet.Files[fi].Tracks {
			if t.Index01 >= 0 {
				kept = append(kept, t)
			}
		}
		sheet.Files[fi].Tracks = kept
		tracks += len(kept)
	}
	if tracks == 0 {
		return nil, fmt.Errorf("cue: no playable tracks")
	}
	return sheet, nil
}

// splitCueLine splits a line into words, respecting quotes.
func splitCueLine(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			has = true
		case (r == ' ' || r == '\t' || r == '\r') && !inQuote:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// CueTimeToFrames converts mm:ss:ff to CD frames (75 frames per second).
func CueTimeToFrames(s string) (int64, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad cue time %q", s)
	}
	var v [3]int64
	for i, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("bad cue time %q", s)
		}
		v[i] = n
	}
	// a frame >= 75 occurs in broken rips (seen live: "06:34:94"), so tolerate it
	// and count it as is; seconds >= 60 are clearly garbage
	if v[1] >= 60 || v[2] >= 100 {
		return 0, fmt.Errorf("bad cue time %q", s)
	}
	return (v[0]*60+v[1])*75 + v[2], nil
}

// FramesToSamples: a CD frame is 1/75 s; divides evenly for all standard sample rates.
func FramesToSamples(frames int64, sampleRate uint32) uint64 {
	return uint64(frames) * uint64(sampleRate) / 75
}

// LayoutCueFile lays out the tracks of one FILE as sample ranges.
// A track runs from its INDEX 01 to the next track's INDEX 01 (the next track's pregap
// stays in the tail of the previous one, as in foobar2000); the last one runs to the end of the file.
func LayoutCueFile(tracks []CueTrack, sampleRate uint32, totalSamples uint64) []CueSpan {
	spans := make([]CueSpan, 0, len(tracks))
	for i, t := range tracks {
		start := FramesToSamples(t.Index01, sampleRate)
		end := totalSamples
		if i+1 < len(tracks) {
			end = FramesToSamples(tracks[i+1].Index01, sampleRate)
		}
		if totalSamples > 0 && end > totalSamples {
			end = totalSamples
		}
		if start >= end {
			continue
		}
		spans = append(spans, CueSpan{Number: t.Number, Start: start, End: end})
	}
	return spans
}
