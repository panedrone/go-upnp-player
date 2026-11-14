//go:build qt

package main

import (
	"strings"

	qt "github.com/mappu/miqt/qt6"
)

// The transport's icons are the page's (BarIcon in web/src/App.tsx), drawn from SVG in the
// theme's text colour. Font symbols (⏮ ⏹ ⏭) came out as blue Windows emoji.
var barIcons = map[string]string{
	"prev":  `<path d="M4 3v10"/><path d="M13 3.5v9L6.5 8z" fill="C"/>`,
	"play":  `<path d="M5 3v10l8-5z" fill="C"/>`,
	"pause": `<path d="M5.5 3.5v9M10.5 3.5v9" stroke-width="2.2"/>`,
	"stop":  `<rect x="3.5" y="3.5" width="9" height="9" rx="1" fill="C"/>`,
	"next":  `<path d="M12 3v10"/><path d="M3 3.5v9L9.5 8z" fill="C"/>`,
}

// iconColor is the theme's text colour (look.go).
func iconColor(theme string) string {
	if theme == "light" {
		return "#212529"
	}
	return "#dee2e6"
}

// barIcon draws one of barIcons at twice the size it is shown, for a sharp picture.
func barIcon(name, color string) *qt.QIcon {
	shape := strings.ReplaceAll(barIcons[name], `"C"`, `"`+color+`"`)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 16 16" fill="none" stroke="` + color +
		`" stroke-width="1.4" stroke-linejoin="round" stroke-linecap="round">` + shape + `</svg>`
	pix := qt.NewQPixmap()
	pix.LoadFromData4([]byte(svg), "SVG")
	return qt.NewQIcon2(pix)
}

// paintTransport draws the transport's icons in the theme's colour; again on a theme change.
func (u *window) paintTransport() {
	c := iconColor(u.ui.Theme)
	for name, b := range u.transport {
		if b == u.play {
			continue
		}
		b.SetIcon(barIcon(name, c))
	}
	if u.playing {
		u.play.SetIcon(barIcon("pause", c))
	} else {
		u.play.SetIcon(barIcon("play", c))
	}
}

// iconButton is the side of every button without a label: one square size everywhere.
const iconButton = 28

// square makes a button without a label the one square size.
func square(b *qt.QPushButton) { b.SetFixedSize2(iconButton, iconButton) }
