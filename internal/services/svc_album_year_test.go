package services

import (
	"path/filepath"
	"testing"
)

func TestAlbumYear(t *testing.T) {
	p := func(parts ...string) string { return filepath.Join(parts...) }
	cases := []struct {
		leaf, date, want string
	}{
		// a reissue: the album's year from its folder, not the reissue's DATE
		{p(`D:\`, "Scorpions (1972-2015)", "Albums", "1984 Love At First Sting", "1984 Love At First Sting [2015 EU 538159342 BMG]", "CD", "Scorpions - Love At First Sting.cue"), "2015", "1984"},
		{p(`D:\`, "Black Sabbath", "1978 Never Say Die!", "1978 Never Say Die! [2009 Germany 2716533 Sanctuary]", "Never Say Die!.cue"), "2009", "1978"},
		{p(`D:\`, "Napalm Death", "Albums", "(1987) Scum", "01 - Instinct Of Survival.flac"), "", "1987"},
		{p(`D:\`, "Scorpions - 2022 - Rock Believer [0602438808168]", "01 - Gas In The Tank.flac"), "2022-02-25", "2022"},
		{p(`D:\`, "Yello - Point [2020]", "01 - Waba Duba.flac"), "2023", "2020"},
		// a bracket naming a release is not the album's year
		{p(`D:\`, "Music", "Some Album [2015 EU 538159342 BMG]", "track.flac"), "2001", "2001"},
		// no year in the folders: the DATE tag's
		{p(`D:\`, "Music", "Some Album", "track.flac"), "2001-05-01", "2001"},
		{p(`D:\`, "Music", "Some Album", "track.flac"), "", ""},
		// a number that is not a year at the start of a name
		{p(`D:\`, "Music", "1000 Hz Legend", "track.flac"), "1999", "1999"},
	}
	for _, c := range cases {
		if got := AlbumYear(c.leaf, c.date); got != c.want {
			t.Errorf("AlbumYear(%q, %q) = %q, want %q", c.leaf, c.date, got, c.want)
		}
	}
}

func TestAlbumYears(t *testing.T) {
	for _, c := range []struct{ year, date, want string }{
		{"1984", "2015", "1984 / 2015"},
		{"1984", "2015-03-02", "1984 / 2015"},
		{"1984", "1984", "1984"},
		{"1984", "", "1984"},
		{"", "2015", "2015"},
		{"", "", ""},
	} {
		if got := AlbumYears(c.year, c.date); got != c.want {
			t.Errorf("AlbumYears(%q, %q) = %q, want %q", c.year, c.date, got, c.want)
		}
	}
}
