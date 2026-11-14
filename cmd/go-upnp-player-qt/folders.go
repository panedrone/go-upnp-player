//go:build qt

package main

import (
	"fmt"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/library"
)

// folders is the Library folders card under the equalizer, as on the page: the folders one
// per line, Save (saves and rescans) and Rescan, the track count in the header.
type folders struct {
	count *qt.QLabel
	text  *qt.QPlainTextEdit
	msg   *qt.QLabel
	scan  *qt.QPushButton
}

func (u *window) buildFolders() *qt.QWidget {
	f := &u.folders
	card := qt.NewQWidget2()
	col := qt.NewQVBoxLayout(card)
	col.SetContentsMargins(0, 8, 0, 0)

	head := qt.NewQHBoxLayout2()
	head.AddWidget(qt.NewQLabel3("Library folders").QWidget)
	head.AddStretch()
	f.count = qt.NewQLabel2()
	f.count.SetStyleSheet("color: gray")
	head.AddWidget(f.count.QWidget)
	fold := qt.NewQPushButton3("▾")
	square(fold)
	head.AddWidget(fold.QWidget)
	col.AddLayout(head.QLayout)

	body := qt.NewQWidget2()
	b := qt.NewQVBoxLayout(body)
	b.SetContentsMargins(0, 0, 0, 0)
	f.text = qt.NewQPlainTextEdit2()
	f.text.SetPlaceholderText(`D:\Music`)
	f.text.SetStyleSheet("font-family: Consolas, monospace")
	f.text.SetFixedHeight(72)
	f.text.SetPlainText(strings.Join(config.Get().Roots, "\n"))
	b.AddWidget(f.text.QWidget)
	hint := qt.NewQLabel3("One folder per line.")
	hint.SetStyleSheet("color: gray")
	b.AddWidget(hint.QWidget)

	row := qt.NewQHBoxLayout2()
	save := qt.NewQPushButton3("Save")
	save.OnClicked(u.saveFolders)
	f.scan = qt.NewQPushButton3("Rescan")
	f.scan.OnClicked(func() {
		go library.Scan(config.Get().Roots)
		u.folderMsg(true, "Rescanning.")
	})
	row.AddWidget(save.QWidget)
	row.AddWidget(f.scan.QWidget)
	row.AddStretch()
	f.msg = qt.NewQLabel2()
	row.AddWidget(f.msg.QWidget)
	b.AddLayout(row.QLayout)
	col.AddWidget(body)

	u.foldable("folders", "the library folders", fold, body)
	return card
}

// saveFolders saves the folders and rescans them; the tree reloads when the scan is done.
func (u *window) saveFolders() {
	var roots []string
	for _, line := range strings.Split(u.folders.text.ToPlainText(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			roots = append(roots, line)
		}
	}
	if err := config.Update(func(c *config.Config) { c.Roots = roots }); err != nil {
		u.folderMsg(false, err.Error())
		return
	}
	go library.Scan(roots)
	u.folderMsg(true, "Saved, rescanning.")
}

func (u *window) folderMsg(ok bool, text string) {
	color := "#198754"
	if !ok {
		color = "#dc3545"
	}
	u.folders.msg.SetStyleSheet("color: " + color)
	u.folders.msg.SetText(text)
}

// showLibrary puts the library's state in the card's header.
func (u *window) showLibrary(scanning bool, tracks int) {
	if scanning {
		u.folders.count.SetText("scanning…")
	} else {
		u.folders.count.SetText(fmt.Sprintf("%d tracks", tracks))
	}
	u.folders.scan.SetEnabled(!scanning)
}
