package services

import (
	"path/filepath"
	"regexp"
)

// The album's year, as the library's folders name it: "1984 Love At First Sting [2015 EU …]",
// "(1987) Scum", "Scorpions - 2022 - Rock Believer", "Yello - Point [2020]". The DATE tag of a reissue is the year of
// the reissue (2015 for that 1984 album); the folders keep the album's own.

var (
	// a year at the start of the name: "1984 …", "(1987) …", "[1978] …"
	leadingYear = regexp.MustCompile(`^[(\[]?((?:19|20)\d{2})[)\]]?(?:\s|$|[-_.])`)
	// a year between dashes: "Artist - 2022 - Album"
	dashedYear = regexp.MustCompile(`\s-\s((?:19|20)\d{2})\s-\s`)
	// a year alone in brackets: "Yello - Point [2020]"; "[2015 EU 538159342 BMG]" names a release
	bracketYear = regexp.MustCompile(`[\[(]((?:19|20)\d{2})[\])]`)
	// the first year in a DATE tag: "2015", "2015-03-02"
	dateYear = regexp.MustCompile(`(?:19|20)\d{2}`)
)

// AlbumYear is the album's year for a track at leaf (its cue sheet or file): the nearest folder
// above it whose name carries one, else the year of the DATE tag, else "".
func AlbumYear(leaf, date string) string {
	for dir := filepath.Dir(leaf); ; {
		name := filepath.Base(dir)
		if m := leadingYear.FindStringSubmatch(name); m != nil {
			return m[1]
		}
		if m := dashedYear.FindStringSubmatch(name); m != nil {
			return m[1]
		}
		if m := bracketYear.FindStringSubmatch(name); m != nil {
			return m[1]
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	return dateYear.FindString(date)
}

// AlbumYears is how an album header names its years: the album's, then the release's after a
// slash when they differ ("1984 / 2015" for a 2015 reissue of a 1984 album), or the one year.
func AlbumYears(year, date string) string {
	release := dateYear.FindString(date)
	switch {
	case year == "":
		return release
	case release == "" || release == year:
		return year
	default:
		return year + " / " + release
	}
}
