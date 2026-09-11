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

import (
	"fmt"
	"os"
	"time"
)

// Ready says whether the successor with this pid is serving — the caller's definition, because what
// "serving" means (a published record, a handshake, the same lineage) belongs to the daemon, not to
// the package that starts processes.
//
// Only Windows asks it. There the previous generation is still alive after starting its successor,
// and CLIENT_LIFECYCLE §4 requires it to stay until the successor is ready: leaving the moment
// Start() succeeds meant a successor that died on its first line was seen by nobody. On Unix the
// image is replaced in place, so there is no previous generation left to ask anything.
type Ready func(pid int) bool

// ReadyWithin is how long the previous generation waits for its successor to be serving: §5's thirty
// seconds from spawn for a new process, the same number the clients use.
var ReadyWithin = 30 * time.Second

// SuccessorDied says the successor ended before it was ever serving, with this process still here.
//
// Code 0 is its own case and the caller has to treat it so: a successor that stopped on purpose —
// its owner closed the pipe, somebody asked it to shut down — also ends before it serves, and that
// is not a build falling over.
type SuccessorDied struct {
	PID  int
	Code int
	// Err is set only when there is no exit code to report — waiting on the successor itself failed
	// — and Code is then -1.
	Err error
}

func (e *SuccessorDied) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("the successor (pid %d) ended before it was serving, and its exit could not be read: %v", e.PID, e.Err)
	}
	return fmt.Sprintf("the successor (pid %d) exited with code %d before it was serving", e.PID, e.Code)
}

func (e *SuccessorDied) Unwrap() error { return e.Err }

// NotReady says the successor is still running but was not serving within ReadyWithin.
//
// Deliberately NOT a failure the successor is killed for. The likeliest cause on Windows is a
// freshly-replaced executable being scanned before it is allowed to run, and killing a build for
// being slow would turn an antivirus pause into a failed update. It is left to come up; the update
// journal's stable window is what judges it from there.
type NotReady struct {
	PID    int
	Within time.Duration
}

func (e *NotReady) Error() string {
	return fmt.Sprintf("the successor (pid %d) was not serving within %s — leaving it to come up", e.PID, e.Within)
}

// Reexec replaces this process with a fresh run of the binary at os.Executable() — the file on disk,
// which an update has already overwritten — carrying the same arguments and environment. It does not
// return on success (on Unix the image is replaced; on Windows a successor is spawned, this process
// waits until ready says it is serving, and then exits). It returns an error when the relaunch could
// not be started — the caller is still running and should carry on: an update that cannot restart
// is a reason to log and keep serving the old build, not to die — and, on Windows only, a
// *SuccessorDied or *NotReady when the successor did not come up. A nil ready means "do not wait",
// which is the old behaviour and the only one Unix has.
func Reexec(ready Ready) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return reexec(exe, os.Args, os.Environ(), ready)
}
