package services

import (
	"path/filepath"
	"strconv"
	"strings"
)

// TrackInfo is a track from the user's point of view: labels and the range in the file.
// End=0 never occurs for whole-file tracks: the full length is substituted.
type TrackInfo struct {
	Number int
	Title  string
	Artist string
	Album  string
	Date   string
	Genre  string
	Start  uint64
	End    uint64
}

// CueTracks returns the tracks of one cue FILE with labels. An empty track field
// is filled from the cue header, then from the file's own tags.
func CueTracks(sheet *CueSheet, file CueFile, sampleRate uint32, nSamples uint64, tags map[string]string) []TrackInfo {
	spans := LayoutCueFile(file.Tracks, sampleRate, nSamples)
	byNum := make(map[int]CueTrack, len(file.Tracks))
	for _, t := range file.Tracks {
		byNum[t.Number] = t
	}
	out := make([]TrackInfo, 0, len(spans))
	for _, sp := range spans {
		t := byNum[sp.Number]
		ti := TrackInfo{
			Number: sp.Number,
			Title:  t.Title,
			Artist: firstNonEmpty(t.Performer, sheet.Performer, tags["ARTIST"], tags["ALBUMARTIST"]),
			Album:  firstNonEmpty(sheet.Title, tags["ALBUM"]),
			Date:   firstNonEmpty(sheet.Date, tags["DATE"]),
			Genre:  firstNonEmpty(sheet.Genre, tags["GENRE"]),
			Start:  sp.Start,
			End:    sp.End,
		}
		if ti.Title == "" {
			ti.Title = "Track " + strconv.Itoa(sp.Number)
		}
		out = append(out, ti)
	}
	return out
}

// TaggedTrack is a plain track file: labels from Vorbis comments;
// if they are missing, the title is taken from the file name.
func TaggedTrack(path string, nSamples uint64, tags map[string]string) TrackInfo {
	num, _ := strconv.Atoi(strings.SplitN(tags["TRACKNUMBER"], "/", 2)[0])
	title := tags["TITLE"]
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return TrackInfo{
		Number: num,
		Title:  title,
		Artist: firstNonEmpty(tags["ARTIST"], tags["ALBUMARTIST"]),
		Album:  tags["ALBUM"],
		Date:   tags["DATE"],
		Genre:  tags["GENRE"],
		Start:  0,
		End:    nSamples,
	}
}

// ResolveCueFile finds the audio file a FILE refers to. A common case after
// EAC: the cue says "album.wav" while "album.flac" sits next to it. So we try
// the same name with .flac, and if the folder has exactly one FLAC, that one.
func ResolveCueFile(cueDir, name string, exists func(string) bool, flacsInDir []string) string {
	direct := filepath.Join(cueDir, name)
	if strings.EqualFold(filepath.Ext(direct), ".flac") && exists(direct) {
		return direct
	}
	alt := strings.TrimSuffix(direct, filepath.Ext(direct)) + ".flac"
	if exists(alt) {
		return alt
	}
	if len(flacsInDir) == 1 {
		return flacsInDir[0]
	}
	return ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
