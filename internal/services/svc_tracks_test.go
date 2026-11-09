package services

import "testing"

func TestCueTracksFillLabelsFromHeaderThenTags(t *testing.T) {
	file := CueFile{Tracks: []CueTrack{
		{Number: 1, Title: "One", Performer: "Guest", Index01: 0},
		{Number: 2, Index01: 75},
	}}
	sheet := &CueSheet{Title: "Sheet Album", Performer: " ", Files: []CueFile{file}}
	tags := map[string]string{"ARTIST": "Tag Artist", "ALBUM": "Tag Album", "DATE": "1999", "GENRE": "Jazz"}
	got := CueTracks(sheet, file, 44100, 88200, tags)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].Artist != "Guest" || got[0].Album != "Sheet Album" || got[0].Date != "1999" || got[0].Genre != "Jazz" {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Title != "Track 2" || got[1].Artist != "Tag Artist" {
		t.Fatalf("%+v", got[1])
	}
	if got[0].End != got[1].Start || got[1].End != 88200 {
		t.Fatalf("%+v", got)
	}
}

func TestCueTracksEmptyFile(t *testing.T) {
	if got := CueTracks(&CueSheet{}, CueFile{}, 44100, 100, nil); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestFirstNonEmptyAllBlank(t *testing.T) {
	if got := firstNonEmpty(" ", ""); got != "" {
		t.Fatalf("%q", got)
	}
}
