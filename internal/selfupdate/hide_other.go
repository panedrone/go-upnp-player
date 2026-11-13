//go:build !windows

package selfupdate

import (
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

// detach starts the restarted player in a session of its own: started from a terminal,
// it would otherwise share the terminal's process group, and a Ctrl+C there meant for the
// old player would kill the new one too.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
