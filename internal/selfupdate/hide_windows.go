//go:build windows

package selfupdate

import (
	"os/exec"
	"syscall"
)

// hideWindow: the player has no console, so a console program it starts would flash a
// window of its own.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// detach: a player started without a console has nothing to detach from.
func detach(*exec.Cmd) {}
