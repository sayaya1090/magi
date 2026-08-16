//go:build windows

package graceful

import (
	"os"
	"os/exec"
	"syscall"
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
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil // unreachable
}
