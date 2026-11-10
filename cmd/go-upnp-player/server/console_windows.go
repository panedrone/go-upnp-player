//go:build windows

package server

import (
	"os"
	"syscall"
)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS (-1)

// HasConsole reports whether output can reach the user. The exe is built with
// -H=windowsgui, so Windows gives it no console of its own: when started from a
// shortcut there is nothing to print to. Started from cmd or PowerShell, it
// attaches to the parent's console and prints there; with the output
// redirected, the inherited handle is used as is.
func HasConsole() bool {
	if t, err := syscall.GetFileType(syscall.Handle(os.Stdout.Fd())); err == nil && t != 0 {
		return true // a console subsystem build, or output redirected to a file/pipe
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return false // started from a shortcut or Explorer: no console
	}
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	os.Stdout, os.Stderr = out, out
	return true
}
