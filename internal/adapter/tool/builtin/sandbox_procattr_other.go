//go:build !windows

package builtin

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/sayaya1090/magi/internal/port"
)

// killCmdTree ends a cancelled command's whole process group, then the shell. The shell runs
// under Setsid (detachTTY), so it leads a group of its own and -pid reaches everything it started.
// Killing the shell alone (the os/exec default this replaced) unblocked cmd.Wait but left its
// children running with init as their parent: a timeout, the person's stop or magi shutting down
// then ended the shell and nothing ever ended the `sleep`/build/server it was waiting on. ESRCH
// from the group means there is no such group (a shell started without Setsid) and the shell
// kill alone is all there is to do. The Windows build does the same with a job object.
func killCmdTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	groupErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	if groupErr != nil && !errors.Is(groupErr, syscall.ESRCH) {
		return groupErr
	}
	return nil
}

// sandboxProcAttr is a no-op off Windows; those platforms confine via an argv
// wrapper (sandbox-exec / bwrap) instead of a process token.
func sandboxProcAttr(spec port.SandboxSpec) *syscall.SysProcAttr { return nil }

// releaseSandbox has nothing to release off Windows. Confinement here is an argv wrapper
// (sandbox-exec, bwrap) and sandboxProcAttr returns nil, so no kernel handle is ever minted. It
// exists so the three spawn paths carry ONE spelling of "let go of the token" rather than a
// Windows-only branch at each — the shape that let the leak sit unnoticed on one platform.
func releaseSandbox(*syscall.SysProcAttr) {}

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

// signalGroup delivers a GRACEFUL signal (SIGINT/SIGTERM) to the whole process
// group — the Ctrl-C affordance for a hung background command: unlike killGroup
// the target gets to run its handlers/cleanup and print what it learned, which
// the agent can then read with bash_output.
func signalGroup(pid int, sig string) error {
	s := syscall.SIGINT
	if sig == "term" {
		s = syscall.SIGTERM
	}
	return syscall.Kill(-pid, s)
}
