package library

import (
	"crypto/sha1"
	"encoding/hex"
	"github.com/panedrone/go-upnp-player/internal/services"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// scanDir walks a folder and returns the number of visible children.
func scanDir(idx *index, dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var cues, flacs []string
	var subdirs []string
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			subdirs = append(subdirs, full)
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".cue":
			cues = append(cues, full)
		case ".flac":
			flacs = append(flacs, full)
		}
	}

	var nodes []Node
	for _, sd := range subdirs {
		if n := scanDir(idx, sd); n > 0 {
			nodes = append(nodes, Node{Path: sd, Name: filepath.Base(sd), Kind: "dir", Count: n})
		}
	}

	covered := map[string]bool{}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	for _, cuePath := range cues {
		tracks := cueTracks(cuePath, flacs, exists, covered)
		if len(tracks) == 0 {
			continue
		}
		idx.tracks = append(idx.tracks, tracks...)
		nodes = append(nodes, Node{Path: cuePath, Name: filepath.Base(cuePath), Kind: "cue", Count: len(tracks)})
	}
	for _, f := range flacs {
		// FLAC files are always shown, like a folder view in foobar2000, even when
		// a cue sheet refers to them (tracks+cue rips, image+cue). Such files are
		// marked Covered: playlists built from a folder and search take the cue
		// tracks instead, so the album does not appear twice.
		var tracks []*Track
		if covered[strings.ToLower(f)] {
			tracks = wholeFileTrack(f)
		} else {
			tracks = fileTracks(f)
		}
		if len(tracks) == 0 {
			continue
		}
		idx.tracks = append(idx.tracks, tracks...)
		kind := "file"
		if len(tracks) > 1 {
			kind = "cue" // embedded cuesheet
		}
		n := Node{Path: f, Name: filepath.Base(f), Kind: kind, Count: len(tracks)}
		if kind == "file" {
			n.ID, n.Number, n.Duration = tracks[0].ID, tracks[0].Number, tracks[0].Duration
		}
		nodes = append(nodes, n)
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		if (nodes[i].Kind == "dir") != (nodes[j].Kind == "dir") {
			return nodes[i].Kind == "dir"
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	if len(nodes) > 0 {
		idx.children[dir] = nodes
	}
	return len(nodes)
}

func cueTracks(cuePath string, flacs []string, exists func(string) bool, covered map[string]bool) []*Track {
	data, err := os.ReadFile(cuePath)
	if err != nil {
		return nil
	}
	sheet, err := services.ParseCue(data)
	if err != nil {
		log.Printf("library: %s: %v", cuePath, err)
		return nil
	}
	var out []*Track
	for _, cf := range sheet.Files {
		audio := services.ResolveCueFile(filepath.Dir(cuePath), cf.Name, exists, flacs)
		if audio == "" {
			continue
		}
		m := fileMeta(audio)
		if m == nil {
			continue
		}
		covered[strings.ToLower(audio)] = true
		for _, ti := range services.CueTracks(sheet, cf, m.SampleRate, m.NSamples, m.Tags) {
			out = append(out, newTrack(cuePath, audio, ti, m, true))
		}
	}
	return out
}

// wholeFileTrack is a FLAC file shown as one track regardless of any embedded
// cue sheet, marked Covered (an external cue sheet already describes it).
func wholeFileTrack(path string) []*Track {
	m := fileMeta(path)
	if m == nil {
		return nil
	}
	t := newTrack(path, path, services.TaggedTrack(path, m.NSamples, m.Tags), m, false)
	t.Covered = true
	return []*Track{t}
}

func fileTracks(path string) []*Track {
	m := fileMeta(path)
	if m == nil {
		return nil
	}
	// cue embedded in the CUESHEET tag (this is how foobar2000 writes it)
	if text := m.Tags["CUESHEET"]; text != "" {
		if sheet, err := services.ParseCue([]byte(text)); err == nil && len(sheet.Files) > 0 {
			var out []*Track
			for _, ti := range services.CueTracks(sheet, sheet.Files[0], m.SampleRate, m.NSamples, m.Tags) {
				out = append(out, newTrack(path, path, ti, m, true))
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []*Track{newTrack(path, path, services.TaggedTrack(path, m.NSamples, m.Tags), m, false)}
}

func newTrack(leaf, audio string, ti services.TrackInfo, m *FileMeta, fromCue bool) *Track {
	h := sha1.Sum([]byte(strings.ToLower(leaf) + "#" + strconv.Itoa(ti.Number) + "#" + strconv.FormatUint(ti.Start, 10)))
	dur := 0.0
	if m.SampleRate > 0 {
		dur = float64(ti.End-ti.Start) / float64(m.SampleRate)
	}
	return &Track{
		ID: hex.EncodeToString(h[:8]), Leaf: leaf, Path: audio, Number: ti.Number,
		Title: ti.Title, Artist: ti.Artist, Album: ti.Album, Date: ti.Date, Genre: ti.Genre,
		Start: ti.Start, End: ti.End, SampleRate: m.SampleRate, Channels: m.Channels, Bits: m.Bits,
		Duration: dur, Seekable: m.Seekable, FromCue: fromCue, FileSize: m.Size, FileFrames: m.NSamples,
	}
}

func fileMeta(path string) *FileMeta {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	key := strings.ToLower(path)
	metaCacheMu.Lock()
	c := metaCache[key]
	metaCacheMu.Unlock()
	if c != nil && c.ModTime == st.ModTime().UnixNano() && c.Size == st.Size() {
		return c
	}
	m, err := readFlacMeta(path, st)
	if err != nil {
		log.Printf("library: %v", err)
		return nil
	}
	if m.SampleRate == 0 || m.NSamples == 0 {
		log.Printf("library: %s: unknown length, skipped", path)
		return nil
	}
	metaCacheMu.Lock()
	metaCache[key] = m
	metaCacheMu.Unlock()
	return m
}
