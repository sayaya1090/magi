//go:build windows

package graceful

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/sayaya1090/magi/internal/quietconsole"
)

// reexec spawns the binary at path as a successor and exits this process — Windows has no execve, so
// the image cannot be replaced in place. The daemon has already released its socket and lock, so the
// successor binds fresh; the sub-second window where the socket is down is the cost of not having
// execve, and a client rides it out by retrying. The successor gets its own process group so a
// console Ctrl-C aimed at the (already exiting) parent's group does not take it down too; it still
// shares the console's std handles, which is what a person watching the window expects. os.Exit(0)
// matches the Unix contract that reexec does not return on success. Only a failure to START the
// successor returns an error, with this process still running.
func reexec(path string, argv []string, env []string) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = reexecAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil // unreachable
}

// reexecAttr keeps the successor in the console situation the parent is in.
//
// In a terminal the parent has a console and the successor inherits it — a person watching the window
// keeps watching. A daemon started detached has NO console, and a console program started by such a
// parent is given a brand-new VISIBLE console by Windows, titled with the exe path. That was the black
// "magi.exe" window that stayed on the taskbar after every "restarting onto the binary on disk"
// (2026-09-06, LTSC 2021 machine: conhost titled C:\...\magi\ppt\magi.exe, born the moment the daemon
// restarted). So without a console the successor is started DETACHED_PROCESS, exactly as
// cmd/magi/detach.go starts the first daemon — no console, stdout/stderr still the inherited log file.
func reexecAttr() *syscall.SysProcAttr {
	flags := uint32(syscall.CREATE_NEW_PROCESS_GROUP)
	if !quietconsole.HasConsole() {
		flags |= windows.DETACHED_PROCESS
	}
	return &syscall.SysProcAttr{CreationFlags: flags}
}
