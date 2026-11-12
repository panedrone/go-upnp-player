//go:build windows

// Package power keeps the computer from sleeping while music plays: the stream comes
// from this process, so a sleeping PC cuts the renderer off mid-track.
package power

import "syscall"

var procSetThreadExecutionState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")

const esSystemRequired = 0x00000001 // ES_SYSTEM_REQUIRED

// StayAwake resets the system idle timer once, like user input does; the display is
// left alone. Without ES_CONTINUOUS nothing stays set on the calling thread, so it
// does not matter which OS thread the goroutine is on, and there is nothing to undo:
// once the calls stop, the PC sleeps by its own settings again.
func StayAwake() {
	_, _, _ = procSetThreadExecutionState.Call(esSystemRequired)
}
