//go:build !windows

package daemon

import (
	"errors"
	"syscall"
)

// processAlive answers whether a pid is running, and whether the answer is worth anything.
//
// Signal 0 delivers nothing and checks the same permissions a real signal would, which is the
// portable way to ask. EPERM means it exists and belongs to somebody else — alive, from the only
// angle that matters here. Anything unexpected comes back as "not known", because a wrong "it is
// gone" is what sends somebody deleting a lock file.
func processAlive(pid int) (alive, known bool) {
	if pid <= 0 {
		return false, false
	}
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return true, true
	case errors.Is(err, syscall.ESRCH):
		return false, true
	default:
		return false, false
	}
}
