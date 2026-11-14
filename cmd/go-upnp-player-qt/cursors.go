//go:build qt

package main

import (
	qt "github.com/mappu/miqt/qt6"
)

// Everything a click acts on shows the hand cursor, as on the page: the buttons and the
// switch, the rows of the playlist, the search results and the devices, and the tree's →.

func hand() *qt.QCursor  { return qt.NewQCursor2(qt.PointingHandCursor) }
func arrow() *qt.QCursor { return qt.NewQCursor2(qt.ArrowCursor) }

// handCursors gives the hand to every button and check box under w.
func handCursors(w *qt.QObject) {
	for _, c := range w.Children() {
		if c.Inherits("QAbstractButton") {
			qt.UnsafeNewQWidget(c.UnsafePointer()).SetCursor(hand())
		}
		handCursors(c)
	}
}

// setCursors is called once the window is built.
func (u *window) setCursors() {
	handCursors(u.w.QObject)
	// a click anywhere on these rows acts: plays the track, shows a result in the tree, uses
	// the device
	u.list.Viewport().SetCursor(hand())
	u.find.hits.Viewport().SetCursor(hand())
	u.devices.Viewport().SetCursor(hand())
	// in the tree only the → makes a playlist; a name is just a name
	u.tree.SetMouseTracking(true)
	u.tree.OnItemEntered(func(_ *qt.QTreeWidgetItem, column int) {
		if column == 1 {
			u.tree.Viewport().SetCursor(hand())
		} else {
			u.tree.Viewport().SetCursor(arrow())
		}
	})
	u.tree.OnViewportEntered(func() { u.tree.Viewport().SetCursor(arrow()) })
}

// clickJumps makes a click on a slider's groove put the handle there and start a drag, as
// the page's range inputs do; Qt's own click only steps the handle by a page. The release
// commits, as after a drag. The hand says it can be clicked.
func clickJumps(s *qt.QSlider) {
	s.SetCursor(hand())
	s.OnMousePressEvent(func(super func(ev *qt.QMouseEvent), ev *qt.QMouseEvent) {
		if ev.Button() == qt.LeftButton {
			p := ev.Position()
			if s.Orientation() == qt.Horizontal {
				s.SetValue(qt.QStyle_SliderValueFromPosition(s.Minimum(), s.Maximum(), int(p.X()), s.Width()))
			} else {
				s.SetValue(qt.QStyle_SliderValueFromPosition2(s.Minimum(), s.Maximum(), int(p.Y()), s.Height(), true))
			}
		}
		super(ev) // the handle is under the pointer now: a press on it, which drags
	})
}
