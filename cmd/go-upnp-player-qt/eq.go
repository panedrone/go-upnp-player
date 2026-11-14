//go:build qt

package main

import (
	"fmt"
	"os"
	"path/filepath"

	qt "github.com/mappu/miqt/qt6"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/player"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// eq is the Equalizer card under the devices, as on the page: an On switch, 18 vertical
// sliders (±20 dB) with the gain above and the band below, the presets as quiet flat
// buttons, Load… and Save… for foobar2000 .feq presets. It is applied on the player's side,
// in the stream the renderer pulls; every change is saved (player.SetEQ).
type eq struct {
	on      *qt.QCheckBox
	body    *qt.QWidget
	sliders []*qt.QSlider
	gains   []*qt.QLabel
	set     services.EQSettings
	preset  string // the last loaded .feq file name, offered back when saving
}

func (u *window) buildEQ() *qt.QWidget {
	e := &u.eq
	e.set = config.Get().EQ.Normalize()
	e.preset = "equalizer.feq"

	card := qt.NewQWidget2()
	col := qt.NewQVBoxLayout(card)
	col.SetContentsMargins(0, 8, 0, 0)

	head := qt.NewQHBoxLayout2()
	head.AddWidget(qt.NewQLabel3(fmt.Sprintf("Equalizer  ±%g dB", services.EQMaxGain)).QWidget)
	head.AddStretch()
	e.on = qt.NewQCheckBox3("On")
	e.on.SetChecked(e.set.Enabled)
	e.on.OnToggled(func(on bool) {
		e.set.Enabled = on
		u.applyEQ()
	})
	head.AddWidget(e.on.QWidget)
	fold := qt.NewQPushButton3("▾")
	square(fold)
	head.AddWidget(fold.QWidget)
	col.AddLayout(head.QLayout)

	e.body = qt.NewQWidget2()
	body := qt.NewQVBoxLayout(e.body)
	body.SetContentsMargins(0, 0, 0, 0)
	grid := qt.NewQGridLayout2()
	grid.SetHorizontalSpacing(0)
	// the wheel and the keys move a slider a step at a time: saved once they pause, not on
	// every step (each save takes playing music to a stream with the new settings)
	settle := qt.NewQTimer()
	settle.SetSingleShot(true)
	settle.OnTimeout(u.applyEQ)
	for i, hz := range services.EQBands {
		gain := qt.NewQLabel2()
		gain.SetAlignment(qt.AlignCenter)
		s := qt.NewQSlider3(qt.Vertical)
		s.SetRange(-int(services.EQMaxGain), int(services.EQMaxGain))
		s.SetMinimumHeight(140)
		s.SetMinimumWidth(12)
		s.SetToolTip(fmt.Sprintf("%g Hz", hz))
		s.OnValueChanged(func(v int) {
			e.set.Gains[i] = float64(v)
			u.showGain(i)
			if !s.IsSliderDown() {
				settle.Start(400)
			}
		})
		s.OnSliderReleased(u.applyEQ)
		band := qt.NewQLabel3(services.FmtBand(hz))
		band.SetAlignment(qt.AlignCenter)
		band.SetStyleSheet("color: gray; font-size: 8pt")
		gain.SetStyleSheet("font-size: 8pt")
		// the labels do not set the column's width: in a narrow one they are cut, the
		// sliders stay
		band.SetSizePolicy2(qt.QSizePolicy__Ignored, qt.QSizePolicy__Preferred)
		gain.SetSizePolicy2(qt.QSizePolicy__Ignored, qt.QSizePolicy__Preferred)
		grid.AddWidget4(gain.QWidget, 0, i, qt.AlignCenter)
		grid.AddWidget4(s.QWidget, 1, i, qt.AlignHCenter)
		grid.AddWidget4(band.QWidget, 2, i, qt.AlignCenter)
		e.sliders = append(e.sliders, s)
		e.gains = append(e.gains, gain)
	}
	body.AddLayout(grid.QLayout)

	// presets as quiet flat buttons: the equalizer is a side tool, not the main thing
	// the presets as on the page: quiet words in a row, Load… and Save… at its right end; a row
	// too narrow for them wraps, so that they do not set the column's width (presetRow)
	row := qt.NewQWidget2()
	var lead, tail []*qt.QPushButton
	link := func(text string, out *[]*qt.QPushButton) *qt.QPushButton {
		b := qt.NewQPushButton5(text, row)
		b.SetFlat(true)
		b.SetStyleSheet("QPushButton { border: none; padding: 2px 5px; }")
		*out = append(*out, b)
		return b
	}
	for _, name := range services.EQPresetNames {
		link(name, &lead).OnClicked(func() {
			g, _ := services.EQPreset(name)
			u.setGains(g)
		})
	}
	load := link("Load…", &tail)
	load.SetToolTip("Load a foobar2000 .feq preset")
	load.OnClicked(u.loadFeq)
	save := link("Save…", &tail)
	save.SetToolTip("Save as a foobar2000 .feq preset")
	save.OnClicked(u.saveFeq)
	row.SetMinimumWidth(60)
	row.OnResizeEvent(func(super func(event *qt.QResizeEvent), event *qt.QResizeEvent) {
		super(event)
		if h := presetRow(row.Width(), lead, tail); row.MinimumHeight() != h {
			row.SetMinimumHeight(h)
		}
	})
	body.AddWidget(row)
	col.AddWidget(e.body)

	u.foldable("eq", "the equalizer", fold, e.body)
	u.showEQ()
	return card
}

// showEQ puts the settings on the sliders; the sliders are off while the equalizer is.
func (u *window) showEQ() {
	e := &u.eq
	for i, s := range e.sliders {
		s.BlockSignals(true)
		s.SetValue(int(e.set.Gains[i]))
		s.BlockSignals(false)
		u.showGain(i)
	}
	e.body.SetEnabled(e.set.Enabled)
}

func (u *window) showGain(i int) {
	g := u.eq.set.Gains[i]
	text := "·"
	if g != 0 {
		text = fmt.Sprintf("%+g", g)
	}
	u.eq.gains[i].SetText(text)
}

func (u *window) setGains(g []float64) {
	copy(u.eq.set.Gains, g)
	u.applyEQ()
}

// applyEQ saves and applies the settings off the GUI thread: while music plays, the
// renderer is taken to the same place in a stream with the new settings.
func (u *window) applyEQ() {
	u.showEQ()
	s := services.EQSettings{Enabled: u.eq.set.Enabled, Gains: append([]float64(nil), u.eq.set.Gains...)}
	u.run(func() error { return player.SetEQ(s) })
}

func (u *window) loadFeq() {
	path := qt.QFileDialog_GetOpenFileName4(u.w.QWidget, "Load an equalizer preset", "", "foobar2000 EQ preset (*.feq)")
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err == nil {
		var g []float64
		if g, err = services.ParseFeq(string(b)); err == nil {
			u.eq.preset = filepath.Base(path)
			u.setGains(g)
			return
		}
	}
	u.errText.SetText(err.Error())
}

func (u *window) saveFeq() {
	path := qt.QFileDialog_GetSaveFileName4(u.w.QWidget, "Save the equalizer preset", u.eq.preset, "foobar2000 EQ preset (*.feq)")
	if path == "" {
		return
	}
	if err := os.WriteFile(path, []byte(services.FormatFeq(u.eq.set.Gains)), 0o644); err != nil {
		u.errText.SetText(err.Error())
		return
	}
	u.eq.preset = filepath.Base(path)
}

// presetRow lays the presets out from the left, wrapping to a new line where one is full, and
// puts tail (Load…, Save…) at the right end of the last line, or of a line of their own when
// they do not fit after the presets. It returns the height the lines take.
func presetRow(width int, lead, tail []*qt.QPushButton) int {
	x, y, line := 0, 0, 0
	place := func(b *qt.QPushButton, w, h int) {
		if x > 0 && x+w > width {
			x, y = 0, y+line
			line = 0
		}
		b.Move(x, y)
		b.Resize(w, h)
		x += w
		line = max(line, h)
	}
	for _, b := range lead {
		hint := b.SizeHint()
		place(b, hint.Width(), hint.Height())
	}
	tailW, tailH := 0, 0
	for _, b := range tail {
		hint := b.SizeHint()
		tailW += hint.Width()
		tailH = max(tailH, hint.Height())
	}
	if x+tailW > width {
		x, y = 0, y+line
		line = 0
	}
	x = max(x, width-tailW) // pushed to the right end, as the page's ms-auto does
	for _, b := range tail {
		hint := b.SizeHint()
		b.Move(x, y)
		b.Resize(hint.Width(), hint.Height())
		x += hint.Width()
	}
	return y + max(line, tailH)
}
