//go:build windows

// Package window shows the UI in a window of its own (WebView2, the Edge engine built into
// Windows 10/11) instead of a browser tab. One window per process; closing it quits the
// player, like the Quit button.
package window

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procLookupIconIDFromDirEx = user32.NewProc("LookupIconIdFromDirectoryEx")
	procCreateIconFromResEx   = user32.NewProc("CreateIconFromResourceEx")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procShowWindow            = user32.NewProc("ShowWindow")
	procIsIconic              = user32.NewProc("IsIconic")
	procSetForegroundWin      = user32.NewProc("SetForegroundWindow")
	procAllowSetForeground    = user32.NewProc("AllowSetForegroundWindow")
)

const (
	smCxIcon   = 11     // SM_CXICON
	smCyIcon   = 12     // SM_CYICON
	smCxSmIcon = 49     // SM_CXSMICON
	smCySmIcon = 50     // SM_CYSMICON
	wmSetIcon  = 0x0080 // WM_SETICON
	iconSmall  = 0
	iconBig    = 1
	swRestore  = 9 // SW_RESTORE
	asfwAny    = ^uintptr(0)
)

// the open window; read from the HTTP and shutdown goroutines
var current atomic.Pointer[webview2.WebView]

// Run shows url in a window and blocks until the window is closed (by its close button
// or by Close). It must be called on the main goroutine with the OS thread locked.
// false: no WebView2 on this machine; the caller opens the browser instead.
func Run(url, title string) bool {
	dataDir := ""
	if dir, err := os.UserCacheDir(); err == nil {
		dataDir = filepath.Join(dir, "go-upnp-player", "webview2")
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  dataDir,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: title, Width: 1360, Height: 900, Center: true,
		},
	})
	if w == nil {
		return false
	}
	setIcon(w.Window())
	restorePlacement(uintptr(w.Window()))
	hookClose(uintptr(w.Window()))
	current.Store(&w)
	w.Navigate(url)
	w.Run() // returns once the window is destroyed or Close posts the quit
	current.Store(nil)
	return true
}

// Close closes the window, if there is one; safe from any goroutine.
func Close() {
	if p := current.Load(); p != nil {
		w := *p
		w.Dispatch(func() {
			savePlacement(uintptr(w.Window())) // Quit or a takeover: the window was not closed by hand
			w.Terminate()
		})
	}
}

// Show brings the window to the front, restoring it if minimized; false when there is
// no window (the player runs in the browser).
func Show() bool {
	p := current.Load()
	if p == nil {
		return false
	}
	w := *p
	w.Dispatch(func() {
		h := uintptr(w.Window())
		if r, _, _ := procIsIconic.Call(h); r != 0 {
			_, _, _ = procShowWindow.Call(h, swRestore)
		}
		_, _, _ = procSetForegroundWin.Call(h)
	})
	return true
}

// AllowForeground lets the running player bring its window to the front: Windows gives
// the foreground only to a process the user just started, which is the second copy that
// asks for the window, not the player that owns it.
func AllowForeground() {
	_, _, _ = procAllowSetForeground.Call(asfwAny)
}

// icon.ico is a copy of assets/icon.ico (go:embed reaches only this folder): the exe
// has no icon resource, so the window gets its icon from these bytes.
//
//go:embed icon.ico
var iconFile []byte

// setIcon gives the window and its taskbar button the player's icon, at the sizes
// Windows asks for.
func setIcon(hwnd unsafe.Pointer) {
	for _, k := range []struct{ kind, metricX, metricY uintptr }{
		{iconBig, smCxIcon, smCyIcon},
		{iconSmall, smCxSmIcon, smCySmIcon},
	} {
		cx, _, _ := procGetSystemMetrics.Call(k.metricX)
		cy, _, _ := procGetSystemMetrics.Call(k.metricY)
		// the offset of the image that suits this size best, within the .ico file
		off, _, _ := procLookupIconIDFromDirEx.Call(uintptr(unsafe.Pointer(&iconFile[0])), 1, cx, cy, 0)
		if off == 0 || int(off) >= len(iconFile) {
			continue
		}
		img := iconFile[off:]
		icon, _, _ := procCreateIconFromResEx.Call(uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)), 1, 0x00030000, cx, cy, 0)
		if icon != 0 {
			_, _, _ = procSendMessageW.Call(uintptr(hwnd), wmSetIcon, k.kind, icon)
		}
	}
}
