//go:build !windows

package builtin

import (
	"syscall"

	"github.com/sayaya1090/magi/internal/port"
)

// sandboxProcAttr is a no-op off Windows; those platforms confine via an argv
// wrapper (sandbox-exec / bwrap) instead of a process token.
func sandboxProcAttr(spec port.SandboxSpec) *syscall.SysProcAttr { return nil }

// detachTTY makes the command run in a new session with no controlling terminal, so a
// program that tries to read from /dev/tty (git credential prompt, ssh host-key
// confirmation, apt, a pager) fails fast instead of hanging until the timeout. It augments
// any sandbox attrs rather than replacing them (off Windows there are none today, but this
// stays correct if that changes).
func detachTTY(attr *syscall.SysProcAttr) *syscall.SysProcAttr {
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.Setsid = true
	return attr
}

// killGroup terminates the whole process group led by pid. Background commands run
// under Setsid, so the leader's pid equals its process-group id; signalling -pid
// reaches any children it forked (a server that spawns workers), not just the shell.
func killGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
