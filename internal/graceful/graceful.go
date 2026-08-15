// Package graceful relaunches the current process onto the binary now on disk — the mechanism a
// daemon uses to finish a self-update by restarting into the new build.
//
// The caller releases first, then relaunches. A daemon drains its connections and gives up its
// socket and lock before calling Reexec (see cmd/magi's daemon loop); the successor then binds
// fresh. A self-triggered update leaves a sub-second window where the socket is unavailable, which a
// client rides out by retrying — acceptable because an update is a rare, deliberate event, not a
// hot path.
//
// The mechanism differs by platform because Windows has no execve: on Unix the process image is
// replaced in place (same PID), on Windows a fresh process is spawned and this one exits. Both are
// hidden behind Reexec, which does not return on success.
package graceful

import "os"

// Reexec replaces this process with a fresh run of the binary at os.Executable() — the file on disk,
// which an update has already overwritten — carrying the same arguments and environment. It does not
// return on success (on Unix the image is replaced; on Windows a successor is spawned and this
// process exits). It returns an error only when the relaunch could not be started, in which case the
// caller is still running and should carry on: an update that cannot restart is a reason to log and
// keep serving the old build, not to die.
func Reexec() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return reexec(exe, os.Args, os.Environ())
}
