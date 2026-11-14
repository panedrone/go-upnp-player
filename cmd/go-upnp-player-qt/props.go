//go:build qt

package main

import (
	"fmt"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/panedrone/go-upnp-player/internal/library"
)

// props is the Properties pane under the playlist: a track's tags and what its FLAC header
// says, as the page shows them. It follows the playing track until an i is clicked.
type props struct {
	view   *qt.QTreeWidget
	id     string // the track shown, or asked for last
	picked bool   // an i was clicked: no longer following the playing track
}

func (u *window) buildProperties() *qt.QWidget {
	v := qt.NewQTreeWidget2()
	v.SetColumnCount(2)
	v.SetHeaderHidden(true)
	v.SetRootIsDecorated(false)
	v.Header().SetStretchLastSection(true)
	v.Header().SetSectionResizeMode2(0, qt.QHeaderView__ResizeToContents)
	u.props.view = v
	return v.QWidget
}

// showProps reads the track's file off the GUI thread; an answer for a track other than the
// one asked for last is dropped.
func (u *window) showProps(id string) {
	u.props.id = id
	go func() {
		t := library.Get(id)
		if t == nil {
			return
		}
		p, err := library.PropertiesOf(t)
		mainthread.Start(func() {
			if u.props.id != id {
				return
			}
			v := u.props.view
			v.Clear()
			if err != nil {
				v.AddTopLevelItem(qt.NewQTreeWidgetItem2([]string{"Error", err.Error()}))
				return
			}
			u.propSection("Metadata", [][2]string{
				{"Artist name", p.Artist},
				{"Track title", p.Title},
				{"Album title", p.Album},
				{"Date", p.Date},
				{"Genre", p.Genre},
				{"Track number", trackNumber(p.Number)},
				{"Comment", p.Comment},
			})
			bitrate := ""
			if p.BitrateKbps > 0 {
				bitrate = fmt.Sprintf("%d kbps", p.BitrateKbps)
			}
			cue := "no"
			if p.EmbeddedCue {
				cue = "yes"
			}
			u.propSection("General", [][2]string{
				{"Duration", fmt.Sprintf("%s (%s samples)", fmtPrecise(p.Duration), grouped(p.Samples))},
				{"Sample rate", fmt.Sprintf("%d Hz", p.SampleRate)},
				{"Channels", fmt.Sprint(p.Channels)},
				{"Bits per sample", fmt.Sprint(p.Bits)},
				{"Bitrate", bitrate},
				{"Codec", p.Codec},
				{"Encoding", p.Encoding},
				{"Tool", p.Tool},
				{"Embedded cuesheet", cue},
				{"Audio MD5", p.MD5},
			})
		})
	}()
}

// propSection adds a bold heading and the rows that have a value.
func (u *window) propSection(title string, rows [][2]string) {
	head := qt.NewQTreeWidgetItem2([]string{title})
	f := head.Font(0)
	f.SetBold(true)
	head.SetFont(0, f)
	u.props.view.AddTopLevelItem(head)
	head.SetFirstColumnSpanned(true)
	for _, r := range rows {
		if r[1] != "" {
			u.props.view.AddTopLevelItem(qt.NewQTreeWidgetItem2([]string{r[0], r[1]}))
		}
	}
}

func trackNumber(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d", n)
}

// fmtPrecise: 4576.507 -> "1:16:16.507"
func fmtPrecise(sec float64) string {
	ms := int64(sec*1000 + 0.5)
	h, m, s, frac := ms/3600000, ms%3600000/60000, ms%60000/1000, ms%1000
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d.%03d", h, m, s, frac)
	}
	return fmt.Sprintf("%d:%02d.%03d", m, s, frac)
}

// grouped: 201823944 -> "201 823 944"
func grouped(n uint64) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
	}
	return b.String()
}
