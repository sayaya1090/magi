//go:build !windows

package graceful

import (
	"os"
	"syscall"
)

// reexec replaces the process image with the binary at path. On success syscall.Exec never returns —
// the same PID now runs the new code. File descriptors without FD_CLOEXEC would be inherited, but the
// daemon has already closed its listener and released its lock before calling this, so there is
// nothing to hand over: the successor binds the socket fresh. On failure the current image is intact
// and the error is returned.
//
// ready is not asked: after a successful exec there is no previous generation left to ask it, and a
// failed one returns before there is a successor.
func reexec(path string, argv []string, env []string, _ Ready) error {
	return syscall.Exec(path, argv, env)
}

// PipeClosed cannot be answered here and is never needed: on Unix a successor replaces this process
// rather than outliving it, so there is no previous generation left to ask whether the owner is
// still holding the pipe.
func PipeClosed(*os.File) (closed, known bool) { return false, false }
