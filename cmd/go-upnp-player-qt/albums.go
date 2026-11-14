//go:build qt

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
)

// The playlist grouped by album, as on the page: a cue sheet or a folder is an album, with a
// header (its cover, its title, the artist and year, its total time) over its tracks. A track
// names its artist only where it differs from the album's.
//
// The header stays put while the tracks scroll, as the page's sticky one does: a header over
// the list shows the album of the topmost visible track. The albums after the first keep a
// header row in the list; scrolled up to the top, it hands over to the one above.

// roleIndex: a track row keeps its queue index; a header row has none.
const roleIndex = int(qt.UserRole) + 2

// roleAlbum: every row keeps the index of its album in window.albums.
const roleAlbum = int(qt.UserRole) + 3

// album is one album of the playlist.
type album struct {
	first *library.Track
	key   string // the cue sheet, or the folder of the files
	total float64
}

// albumHead is the header's widget: the one over the list, and each header row's.
type albumHead struct {
	box   *qt.QWidget
	cover *qt.QLabel
	title *qt.QLabel
	who   *qt.QLabel
	total *qt.QLabel
	shown string // the album it shows (its key)
}

// albumKey is what groups tracks into an album: the cue sheet, or the folder of the files.
func albumKey(t *library.Track) string {
	if t.FromCue {
		return t.Leaf
	}
	return filepath.Dir(t.Leaf)
}

// newAlbumHead builds a header: the cover (64 px), the title in bold and "artist · year" under
// it, 12 px apart as on the page, the total on the right.
func newAlbumHead() *albumHead {
	h := &albumHead{box: qt.NewQWidget2()}
	h.box.SetCursor(arrow()) // the rows play, the header does not
	h.box.SetFixedHeight(80) // the cover and its margins; it must not stretch with the column
	row := qt.NewQHBoxLayout(h.box)
	row.SetContentsMargins(8, 8, 8, 8)
	row.SetSpacing(12)
	h.cover = qt.NewQLabel2()
	h.cover.SetFixedSize2(64, 64)
	row.AddWidget(h.cover.QWidget)
	text := qt.NewQVBoxLayout2()
	text.SetSpacing(12)
	text.AddStretch()
	h.title = qt.NewQLabel2()
	f := h.title.Font()
	f.SetBold(true)
	h.title.SetFont(f)
	text.AddWidget(h.title.QWidget)
	h.who = qt.NewQLabel2()
	h.who.SetStyleSheet("color: gray")
	text.AddWidget(h.who.QWidget)
	text.AddStretch()
	row.AddLayout2(text.QLayout, 1)
	h.total = qt.NewQLabel2()
	h.total.SetStyleSheet("background: rgba(13,110,253,0.25); color: #6ea8fe; border-radius: 4px; padding: 2px 6px")
	h.total.SetSizePolicy2(qt.QSizePolicy__Fixed, qt.QSizePolicy__Fixed) // a badge, not a column
	row.AddWidget3(h.total.QWidget, 0, qt.AlignVCenter)
	return h
}

// show puts an album on the header; the cover comes off the GUI thread.
func (u *window) showHead(h *albumHead, a album) {
	if h.shown == a.key {
		return
	}
	h.shown = a.key
	name := a.first.Album
	if name == "" {
		name = filepath.Base(a.key)
	}
	h.title.SetText(name)
	h.who.SetText(strings.Join(nonEmpty(a.first.Artist, a.first.Years), " · ")) // "1984 / 2015" for a reissue
	h.total.SetText(fmtTime(a.total))
	h.box.SetToolTip(a.key)
	h.cover.Clear()
	h.cover.SetStyleSheet("background: rgba(128,128,128,0.15); border-radius: 4px")
	u.loadCover(h, a)
}

func (u *window) loadPlaylist() {
	u.list.Clear()
	u.playingRow = -1
	u.rows = u.rows[:0]
	u.ids = u.ids[:0]
	u.albums = u.albums[:0]
	_, tracks := player.Queue()
	for start := 0; start < len(tracks); {
		a := album{first: tracks[start], key: albumKey(tracks[start])}
		end := start
		for end < len(tracks) && albumKey(tracks[end]) == a.key {
			a.total += tracks[end].Duration
			end++
		}
		k := len(u.albums)
		u.albums = append(u.albums, a)
		if k > 0 { // the first album's header is the one over the list
			u.addHeaderRow(k)
		}
		for _, t := range tracks[start:end] {
			u.addTrackRow(t, k)
		}
		start = end
	}
	u.head.box.SetVisible(len(u.albums) > 0)
	u.head.shown = ""
	u.stickHead()
}

func (u *window) addTrackRow(t *library.Track, k int) {
	i := len(u.rows)
	num := ""
	if t.Number > 0 {
		num = fmt.Sprint(t.Number)
	}
	title := t.Title
	if t.Artist != "" && t.Artist != u.albums[k].first.Artist {
		title += "  —  " + t.Artist
	}
	item := qt.NewQTreeWidgetItem2([]string{num, title, fmtTime(t.Duration), "ⓘ"})
	item.SetData(0, roleIndex, qt.NewQVariant14(fmt.Sprint(i)))
	item.SetData(0, roleAlbum, qt.NewQVariant14(fmt.Sprint(k)))
	item.SetTextAlignment(0, int(qt.AlignRight|qt.AlignVCenter))
	item.SetTextAlignment(2, int(qt.AlignRight|qt.AlignVCenter))
	item.SetTextAlignment(3, int(qt.AlignCenter))
	item.SetToolTip(3, "Show properties")
	u.list.AddTopLevelItem(item)
	u.rows = append(u.rows, item)
	u.ids = append(u.ids, t.ID)
}

func (u *window) addHeaderRow(k int) {
	item := qt.NewQTreeWidgetItem2([]string{""})
	item.SetFlags(qt.ItemIsEnabled) // not selectable: it plays nothing
	item.SetData(0, roleAlbum, qt.NewQVariant14(fmt.Sprint(k)))
	u.list.AddTopLevelItem(item)
	item.SetFirstColumnSpanned(true)
	h := newAlbumHead()
	u.showHead(h, u.albums[k])
	item.SetSizeHint(0, qt.NewQSize2(0, 80))
	u.list.SetItemWidget(item, 0, h.box)
}

// stickHead shows over the list the album of the topmost visible track: a header row at the
// top hands over to the album above it.
func (u *window) stickHead() {
	if len(u.albums) == 0 {
		return
	}
	k := 0
	if item := u.list.ItemAt2(4, 1); item != nil {
		fmt.Sscan(item.Data(0, roleAlbum).ToString(), &k)
		if _, track := rowIndex(item); !track && k > 0 {
			k--
		}
	}
	if k >= 0 && k < len(u.albums) {
		u.showHead(u.head, u.albums[k])
	}
}

// loadCover reads the album's art off the GUI thread and shows it cropped to the square, as
// the page's object-fit: cover does; a header showing another album by then drops it.
func (u *window) loadCover(h *albumHead, a album) {
	key := a.key
	go func() {
		art := library.FindArt(a.first)
		if art == nil {
			return
		}
		data := art.Data
		if art.Path != "" {
			b, err := os.ReadFile(art.Path)
			if err != nil {
				return
			}
			data = b
		}
		if len(data) == 0 {
			return
		}
		mainthread.Start(func() {
			if h.shown != key {
				return
			}
			pix := qt.NewQPixmap()
			if !pix.LoadFromData(&data[0], uint(len(data))) || pix.IsNull() {
				return
			}
			pix = pix.Scaled3(64, 64, qt.KeepAspectRatioByExpanding, qt.SmoothTransformation)
			h.cover.SetStyleSheet("")
			h.cover.SetPixmap(pix.Copy((pix.Width()-64)/2, (pix.Height()-64)/2, 64, 64))
		})
	}()
}

// rowIndex is a playlist row's queue index; false for an album header.
func rowIndex(item *qt.QTreeWidgetItem) (int, bool) {
	v := item.Data(0, roleIndex).ToString()
	if v == "" {
		return 0, false
	}
	var i int
	_, err := fmt.Sscan(v, &i)
	return i, err == nil
}

// markPlaying makes the playing track's row bold and puts the play arrow left of its number,
// as the page does; the arrow is the Play button's (icons.go).
func (u *window) markPlaying(i int) {
	if i == u.playingRow {
		return
	}
	set := func(i int, bold bool) {
		if i < 0 || i >= len(u.rows) {
			return
		}
		item := u.rows[i]
		f := item.Font(1)
		f.SetBold(bold)
		for c := range 3 {
			item.SetFont(c, f)
		}
		if bold {
			item.SetIcon(0, barIcon("play", iconColor(u.ui.Theme)))
		} else {
			item.SetIcon(0, qt.NewQIcon())
		}
	}
	set(u.playingRow, false)
	set(i, true)
	u.playingRow = i
}
