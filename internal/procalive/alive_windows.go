//go:build windows

package procalive

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Alive answers whether a pid is running, and whether the answer is worth anything — the same two
// facts the unix build answers, and it has to be able to say "gone" as plainly as ESRCH does.
//
// ⚠ **It used to answer `os.FindProcess`, and that cannot say gone.** A process that has ended can
// still be opened for as long as anything holds a handle to it, and a pid that is truly gone comes
// back as an error that the old code read as "cannot tell". Both ways the answer was never "it is
// dead" — and the one caller that acts on that answer, the update journal, only rolls a bad build
// back when the generation watching it is GONE. So on Windows it never rolled one back. Measured
// 2026-09-12 with a real daemon: the candidate was killed, the next start read its watcher as
// "unknown", and settled in to serve on the build that had just fallen over
// (`TestABuildThatDoesNotStayUpIsRolledBackOnWindows`).
//
// The handle's signalled state is the authority, not the exit code: `GetExitCodeProcess` reports
// STILL_ACTIVE (259) for a running process AND for one that exited with 259, which is a legal exit
// code. A process handle is signalled when the process ends and never before, so waiting on it for
// zero milliseconds asks exactly the question.
func Alive(pid int) (alive, known bool) {
	if pid <= 0 {
		return false, false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE,
		false, uint32(pid))
	if err != nil {
		switch {
		case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
			// Windows' ESRCH: no process has that id.
			return false, true
		case errors.Is(err, windows.ERROR_ACCESS_DENIED):
			// It exists and belongs to somebody else — alive, from the only angle that matters here,
			// exactly as EPERM is read on unix.
			return true, true
		default:
			// Anything unexpected is "not known": a wrong "it is gone" is what sends somebody
			// deleting a claim or rolling a good build back.
			return false, false
		}
	}
	defer windows.CloseHandle(h)
	switch s, werr := windows.WaitForSingleObject(h, 0); {
	case werr != nil:
		return false, false
	case s == uint32(windows.WAIT_TIMEOUT):
		return true, true // not signalled: still running
	case s == windows.WAIT_OBJECT_0:
		return false, true // signalled: it has ended, handle or no handle
	default:
		return false, false
	}
}
