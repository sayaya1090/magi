//go:build windows

package graceful

import (
	"os"
	"os/exec"
)

// reexec spawns the binary at path as a detached successor and exits this process — Windows has no
// execve, so the image cannot be replaced in place. The daemon has already released its socket and
// lock, so the successor binds fresh; the sub-second window where the socket is down is the cost of
// not having execve, and a client rides it out by retrying. os.Exit(0) matches the Unix contract that
// reexec does not return on success. Only a failure to START the successor returns an error, with
// this process still running.
func reexec(path string, argv []string, env []string) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil // unreachable
}
