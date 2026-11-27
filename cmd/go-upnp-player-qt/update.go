//go:build qt

package main

import (
	"context"
	"fmt"
	"html"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/selfupdate"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// Check for updates, as the page's cloud button does (ADR-019): GitHub is asked only on the
// click, and the answer shows in the status bar with a × to close it. The Qt copy is offered
// the release page: it has no setup yet, and go install would build the web player, the Qt one
// needing cgo and the Qt SDK.

// buildNote is the status bar's note area: the answer and its ×.
func (u *window) buildNote() {
	u.note = qt.NewQLabel2()
	u.note.SetTextFormat(qt.RichText)
	u.note.SetOpenExternalLinks(true) // the link opens in the system browser
	u.noteClose = qt.NewQPushButton3("×")
	u.noteClose.SetFlat(true)
	square(u.noteClose)
	u.noteClose.SetToolTip("Close")
	u.noteClose.OnClicked(func() { u.showNote("") })
	u.w.StatusBar().AddWidget(u.note.QWidget)
	u.w.StatusBar().AddWidget(u.noteClose.QWidget)
	u.showNote("")
}

// showNote shows rich text in the note area; empty hides it.
func (u *window) showNote(text string) {
	u.note.SetText(text)
	u.note.SetVisible(text != "")
	u.noteClose.SetVisible(text != "")
}

func (u *window) checkUpdate() {
	u.showNote("Checking for updates…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rel, err := selfupdate.Latest(ctx)
		mainthread.Start(func() {
			if err != nil {
				u.showNote(`<span style="color:#dc3545">Could not check for updates: ` + html.EscapeString(err.Error()) + `</span>`)
				return
			}
			o := services.OfferUpdate(player.Version, rel, services.UpdateByPage)
			if !o.Newer {
				u.showNote(fmt.Sprintf("You have the latest version, %s.", html.EscapeString(o.Current)))
				return
			}
			u.showNote(fmt.Sprintf(`go-upnp-player %s is out: <a href="%s">the release page</a>`,
				html.EscapeString(o.Latest), html.EscapeString(o.URL)))
		})
	}()
}
