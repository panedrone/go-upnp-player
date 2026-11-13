//go:build windows

package window

import (
	"log"
	"syscall"
	"unsafe"

	"github.com/panedrone/go-upnp-player/internal/config"
)

// The window comes back where it was closed: its restored rectangle and whether it was
// maximized, kept in config.json. A rectangle no monitor shows any more (a screen
// unplugged) is dropped, and the window opens centred.

var (
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = user32.NewProc("SetWindowPlacement")
	procMonitorFromRect    = user32.NewProc("MonitorFromRect")
	procSetWindowLongPtrW  = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW    = user32.NewProc("CallWindowProcW")
)

const (
	swShowNormal          = 1
	swShowMinimized       = 2
	swShowMaximized       = 3
	wpfRestoreToMaximized = 2
	wmClose               = 0x0010
	gwlpWndProc           = ^uintptr(3) // GWLP_WNDPROC (-4)
	monitorDefaultToNull  = 0
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

// WINDOWPLACEMENT
type windowPlacement struct {
	Length  uint32
	Flags   uint32
	ShowCmd uint32
	MinPos  point
	MaxPos  point
	Normal  rect
}

// restorePlacement puts the window where it was last closed.
func restorePlacement(hwnd uintptr) {
	c := config.Get().Window
	if c == nil || !usable(*c) {
		return
	}
	r := rect{c.Left, c.Top, c.Right, c.Bottom}
	if m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNull); m == 0 {
		return
	}
	wp := windowPlacement{ShowCmd: swShowNormal, Normal: r}
	wp.Length = uint32(unsafe.Sizeof(wp))
	if c.Maximized {
		wp.ShowCmd = swShowMaximized
	}
	_, _, _ = procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
}

// usable: a saved rectangle large enough to be the player's window.
func usable(c config.Window) bool {
	return c.Right-c.Left >= 400 && c.Bottom-c.Top >= 300
}

// savePlacement writes where the window is now; a minimized window is saved as what it
// returns to.
func savePlacement(hwnd uintptr) {
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return
	}
	w := config.Window{
		Left: wp.Normal.Left, Top: wp.Normal.Top, Right: wp.Normal.Right, Bottom: wp.Normal.Bottom,
		Maximized: wp.ShowCmd == swShowMaximized ||
			(wp.ShowCmd == swShowMinimized && wp.Flags&wpfRestoreToMaximized != 0),
	}
	if !usable(w) {
		return
	}
	if err := config.Update(func(c *config.Config) { c.Window = &w }); err != nil {
		log.Printf("window: save placement: %v", err)
	}
}

// the window procedure of go-webview2, called after ours
var origWndProc uintptr

// hookClose sees the close button before go-webview2 destroys the window, while its
// placement can still be read.
func hookClose(hwnd uintptr) {
	origWndProc, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, syscall.NewCallback(closeHook))
}

func closeHook(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == wmClose {
		savePlacement(hwnd)
	}
	r, _, _ := procCallWindowProcW.Call(origWndProc, hwnd, msg, wp, lp)
	return r
}
