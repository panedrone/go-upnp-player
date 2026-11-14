//go:build qt

package main

import (
	"log"
	"sort"

	qt "github.com/mappu/miqt/qt6"

	"github.com/panedrone/go-upnp-player/internal/config"
)

// The window's look and layout as it was left: the theme, its place and size, the splitters
// and the folded cards, kept in the config's ui section (ADR-027).

// loadUI is the remembered state, or an empty one.
func loadUI() config.UI {
	if p := config.Get().UI; p != nil {
		return *p
	}
	return config.UI{}
}

// saveUI writes what changes: fn edits a copy of the remembered state.
func saveUI(fn func(ui *config.UI)) {
	if err := config.Update(func(c *config.Config) {
		ui := config.UI{}
		if c.UI != nil {
			ui = *c.UI
		}
		fn(&ui)
		c.UI = &ui
	}); err != nil {
		log.Printf("config: %v", err)
	}
}

// applyTheme gives the whole application the page's colours (Bootstrap's dark and light) on
// Qt's Fusion style, which follows the palette everywhere.
func applyTheme(theme string) {
	qt.QApplication_SetStyle(qt.QStyleFactory_Create("Fusion"))
	type role struct {
		r qt.QPalette__ColorRole
		c string
	}
	dark := []role{
		{qt.QPalette__Window, "#212529"}, {qt.QPalette__WindowText, "#dee2e6"},
		{qt.QPalette__Base, "#212529"}, {qt.QPalette__AlternateBase, "#2b3035"},
		{qt.QPalette__Text, "#dee2e6"}, {qt.QPalette__Button, "#343a40"},
		{qt.QPalette__ButtonText, "#dee2e6"}, {qt.QPalette__BrightText, "#ffffff"},
		{qt.QPalette__Highlight, "#495057"}, {qt.QPalette__HighlightedText, "#ffffff"},
		{qt.QPalette__ToolTipBase, "#343a40"}, {qt.QPalette__ToolTipText, "#dee2e6"},
		{qt.QPalette__PlaceholderText, "#8c959f"}, {qt.QPalette__Link, "#6ea8fe"},
	}
	light := []role{
		{qt.QPalette__Window, "#f8f9fa"}, {qt.QPalette__WindowText, "#212529"},
		{qt.QPalette__Base, "#ffffff"}, {qt.QPalette__AlternateBase, "#f8f9fa"},
		{qt.QPalette__Text, "#212529"}, {qt.QPalette__Button, "#e9ecef"},
		{qt.QPalette__ButtonText, "#212529"}, {qt.QPalette__BrightText, "#000000"},
		{qt.QPalette__Highlight, "#dee2e6"}, {qt.QPalette__HighlightedText, "#212529"},
		{qt.QPalette__ToolTipBase, "#ffffff"}, {qt.QPalette__ToolTipText, "#212529"},
		{qt.QPalette__PlaceholderText, "#6c757d"}, {qt.QPalette__Link, "#0d6efd"},
	}
	roles := dark
	if theme == "light" {
		roles = light
	}
	p := qt.NewQPalette()
	for _, r := range roles {
		p.SetColor2(r.r, qt.NewQColor6(r.c))
	}
	// disabled controls (the equalizer while it is off) are grayed out
	for _, r := range []qt.QPalette__ColorRole{qt.QPalette__WindowText, qt.QPalette__Text, qt.QPalette__ButtonText} {
		p.SetColor(qt.QPalette__Disabled, r, qt.NewQColor6("#6c757d"))
	}
	qt.QApplication_SetPalette(p)
}

// themeIcon is the theme button's face: the theme it switches to.
func themeIcon(theme string) (string, string) {
	if theme == "light" {
		return "☾", "Switch to the dark theme"
	}
	return "☀", "Switch to the light theme"
}

// foldable makes fold show and hide body, remembered under name.
func (u *window) foldable(name, what string, fold *qt.QPushButton, body *qt.QWidget) {
	set := func(open bool) {
		body.SetVisible(open)
		if open {
			fold.SetText("▾")
			fold.SetToolTip("Hide " + what)
		} else {
			fold.SetText("▸")
			fold.SetToolTip("Show " + what)
		}
	}
	set(!u.ui.Folded[name])
	fold.OnClicked(func() {
		open := !body.IsVisible()
		set(open)
		if u.ui.Folded == nil {
			u.ui.Folded = map[string]bool{}
		}
		u.ui.Folded[name] = !open
		saveUI(func(ui *config.UI) {
			if ui.Folded == nil {
				ui.Folded = map[string]bool{}
			}
			ui.Folded[name] = !open
		})
	})
}

// splitter restores a splitter's sizes, or gives it the default ones.
func (u *window) splitter(name string, s *qt.QSplitter, def []int) {
	u.splits[name] = s
	if sizes := u.ui.Splitters[name]; len(sizes) == len(def) {
		s.SetSizes(sizes)
	} else {
		s.SetSizes(def)
	}
}

// saveLayout writes the window's place and size, the splitters and the tree's open nodes;
// when the window closes or the player is told to quit.
func (u *window) saveLayout() {
	geo := u.w.SaveGeometry()
	sizes := map[string][]int{}
	for name, s := range u.splits {
		sizes[name] = s.Sizes()
	}
	open := []string{} // empty, not nil: all closed is a state of its own
	for p := range u.expanded {
		open = append(open, p)
	}
	sort.Strings(open)
	saveUI(func(ui *config.UI) {
		ui.Geometry = geo
		ui.Splitters = sizes
		if u.expanded != nil { // nil: the tree never had its roots, nothing to say
			ui.Expanded = open
		}
	})
}
