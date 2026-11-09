package services

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

func TestWavLayout(t *testing.T) {
	l := NewWavLayout(44100, 2, 16, 1000)
	if l.BlockAlign() != 4 || l.DataLen() != 4000 || l.TotalLen() != 4044 {
		t.Fatalf("layout: %+v", l)
	}
	h := l.Header()
	if string(h[0:4]) != "RIFF" || binary.LittleEndian.Uint32(h[40:]) != 4000 || binary.LittleEndian.Uint16(h[34:]) != 16 {
		t.Fatalf("header: % x", h)
	}
	// 20-bit FLAC goes into a 24-bit container
	if l := NewWavLayout(96000, 2, 20, 1); l.ContainerBits != 24 || l.BlockAlign() != 6 {
		t.Fatalf("20 bit: %+v", l)
	}
}

func TestLocateOffset(t *testing.T) {
	l := NewWavLayout(44100, 2, 24, 1000) // 6 bytes per sample
	if _, _, in := l.LocateOffset(10); !in {
		t.Fatal("offset 10 is in header")
	}
	s, skip, in := l.LocateOffset(WavHeaderLen + 6*7 + 4)
	if in || s != 7 || skip != 4 {
		t.Fatalf("got %d %d %v", s, skip, in)
	}
}

func TestAppendSamples(t *testing.T) {
	l := NewWavLayout(44100, 2, 16, 2)
	got := l.AppendSamples(nil, [][]int32{{1, -1}, {256, 0}}, 0, 2)
	want := []byte{1, 0, 0, 1, 0xFF, 0xFF, 0, 0}
	if string(got) != string(want) {
		t.Fatalf("got % x", got)
	}
	l20 := NewWavLayout(44100, 1, 20, 1)
	if got := l20.AppendSamples(nil, [][]int32{{1}}, 0, 1); got[0] != 0x10 || got[1] != 0 || got[2] != 0 {
		t.Fatalf("20 bit shift: % x", got)
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		h          string
		start, end int64
		partial    bool
		err        bool
	}{
		{"", 0, 99, false, false},
		{"bytes=0-", 0, 99, true, false},
		{"bytes=10-19", 10, 19, true, false},
		{"bytes=90-200", 90, 99, true, false},
		{"bytes=-10", 90, 99, true, false},
		{"bytes=100-", 0, 0, false, true},
		{"bytes=20-10", 0, 0, false, true},
		{"bytes=0-1,5-6", 0, 99, false, false},
	}
	for _, c := range cases {
		s, e, p, err := ParseRange(c.h, 100)
		if (err != nil) != c.err || (!c.err && (s != c.start || e != c.end || p != c.partial)) {
			t.Errorf("%q: got %d-%d %v %v", c.h, s, e, p, err)
		}
	}
}

func TestHMS(t *testing.T) {
	if got := FormatHMS(402.4134); got != "0:06:42.413" {
		t.Fatal(got)
	}
	if got := FormatSeekTarget(3725.9); got != "1:02:05" {
		t.Fatal(got)
	}
	if got := ParseHMS("1:41:26"); got != 6086 {
		t.Fatal(got)
	}
	if got := ParseHMS("NOT_IMPLEMENTED"); got != 0 {
		t.Fatal(got)
	}
}

func TestBuildDIDLEscapes(t *testing.T) {
	d := BuildDIDL(TrackMeta{Title: "Rock & <Roll>", Mime: "audio/wav", URL: "http://h/a?x=1&y=2", Duration: 1})
	if strings.Contains(d, "Rock & ") || !strings.Contains(d, "Rock &amp; &lt;Roll&gt;") || !strings.Contains(d, "x=1&amp;y=2") {
		t.Fatal(d)
	}
}

func TestPickMime(t *testing.T) {
	beagle := []string{"http-get:*:audio/x-wav:*", "http-get:*:audio/x-flac:*"}
	if got := PickMime(beagle, "audio/wav", "audio/x-wav"); got != "audio/x-wav" {
		t.Fatal(got)
	}
	if got := PickMime(nil, "audio/wav", "audio/x-wav"); got != "audio/wav" {
		t.Fatal(got)
	}
}

func TestDecideTick(t *testing.T) {
	late := StartGrace + time.Second
	cases := []struct {
		name string
		in   TickInput
		want TickAction
	}{
		{"user stopped", TickInput{WantPlaying: false, State: "STOPPED", SinceCmd: late}, TickNone},
		{"renderer moved to next", TickInput{WantPlaying: true, State: "PLAYING", TrackURI: "u2", NextURL: "u2",
			Duration: 388, Position: 1, LastPosition: 229, CurDuration: 230.7, NextDuration: 387.8, SinceCmd: late}, TickAdvanced},
		// gmrender reported next as current early, while the first track is still playing
		{"early next uri", TickInput{WantPlaying: true, State: "PLAYING", TrackURI: "u2", NextURL: "u2",
			Duration: 230, Position: 58, LastPosition: 57, CurDuration: 230.7, NextDuration: 387.8}, TickNone},
		// tracks of equal length: the transition shows up as the position rolling back
		{"same length tracks", TickInput{WantPlaying: true, State: "PLAYING", TrackURI: "u2", NextURL: "u2",
			Duration: 200, Position: 1, LastPosition: 199, CurDuration: 200, NextDuration: 200, SinceCmd: late}, TickAdvanced},
		// early URI switch with RelTime reset, tracks of different length: not a transition
		{"early uri with reltime reset", TickInput{WantPlaying: true, State: "PLAYING", TrackURI: "u2", NextURL: "u2",
			Duration: 230, Position: 2, LastPosition: 90, CurDuration: 230.7, NextDuration: 387.8, SinceCmd: late}, TickNone},
		// the user seeked back: not a transition
		{"seek back", TickInput{WantPlaying: true, State: "PLAYING", TrackURI: "u2", NextURL: "u2",
			Duration: 200, Position: 10, LastPosition: 150, CurDuration: 200, NextDuration: 200, SinceCmd: time.Second}, TickNone},
		{"stopped right after play", TickInput{WantPlaying: true, State: "STOPPED", SinceCmd: time.Second, HasNext: true}, TickNone},
		{"ended with next", TickInput{WantPlaying: true, State: "STOPPED", SinceCmd: late, HasNext: true}, TickPlayNext},
		{"ended last", TickInput{WantPlaying: true, State: "STOPPED", SinceCmd: late}, TickFinished},
		{"playing", TickInput{WantPlaying: true, State: "PLAYING", SinceCmd: late, TrackURI: "u1", NextURL: "u2"}, TickNone},
	}
	for _, c := range cases {
		if got := DecideTick(c.in); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

func TestTaggedTrack(t *testing.T) {
	ti := TaggedTrack(`D:\m\01 - Song.flac`, 100, map[string]string{"TRACKNUMBER": "3/12", "ALBUMARTIST": "X"})
	if ti.Number != 3 || ti.Artist != "X" || ti.End != 100 || !strings.Contains(ti.Title, "Song") {
		t.Fatalf("%+v", ti)
	}
}

func TestPlanSwitch(t *testing.T) {
	cases := []struct {
		state string
		dur   float64
		want  SwitchPlan
	}{
		{"STOPPED", 230, SwitchPlan{}},
		{"NO_MEDIA_PRESENT", 0, SwitchPlan{}},
		{"PLAYING", 230, SwitchPlan{ViaNext: true, SeekTo: 229}},
		{"PAUSED_PLAYBACK", 402, SwitchPlan{ViaNext: true, SeekTo: 401, Resume: true}},
		{"PLAYING", 0, SwitchPlan{}}, // unknown length: nowhere to seek
		{"TRANSITIONING", 230, SwitchPlan{}},
	}
	for _, c := range cases {
		if got := PlanSwitch(c.state, c.dur); got != c.want {
			t.Errorf("%s/%v: got %+v want %+v", c.state, c.dur, got, c.want)
		}
	}
}

func TestSwitchDone(t *testing.T) {
	if !SwitchDone(402, 402.41) || SwitchDone(230, 402.41) || SwitchDone(0, 0) {
		t.Fatal("SwitchDone")
	}
}
