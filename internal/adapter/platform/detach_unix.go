//go:build !windows

package platform

import (
	"os/exec"
	"syscall"
)

// detach makes a captured-output command run in a new session with no controlling
// terminal, so an interactive program (ssh asking for a password, git a credential,
// a pager) that tries to reach the operator by opening /dev/tty gets no tty and fails
// fast instead of seizing the TUI's terminal and corrupting the display. Every
// Exec caller here is a non-interactive command whose output we capture, so detaching
// is always correct. Mirrors the bash tool's own tty detachment.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
