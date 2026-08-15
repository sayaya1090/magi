//go:build !windows

package graceful

import "syscall"

// reexec replaces the process image with the binary at path. On success syscall.Exec never returns —
// the same PID now runs the new code. File descriptors without FD_CLOEXEC would be inherited, but the
// daemon has already closed its listener and released its lock before calling this, so there is
// nothing to hand over: the successor binds the socket fresh. On failure the current image is intact
// and the error is returned.
func reexec(path string, argv []string, env []string) error {
	return syscall.Exec(path, argv, env)
}
