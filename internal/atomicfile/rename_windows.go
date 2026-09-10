package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"
)

// Replace puts the temp file in place of the destination, retrying while a reader holds it.
//
// ⚠ **Rename over an open file is not allowed here.** Go's os.Open on Windows does not ask for
// FILE_SHARE_DELETE, so a destination somebody is reading cannot be replaced: the call fails with
// ERROR_ACCESS_DENIED. Measured 2026-09-11 — the same rename succeeds with no handle open, fails
// while a reader is open, and succeeds again once it closes.
//
// A reader is not an unusual state for the files this package writes; it is the DESIGNED one. The
// session record is polled by every viewer, the published daemon records are read by every fleet
// listing, and `atomicfile.Write` exists so those readers never see half a file. So the write that
// exists to be safe against concurrent readers was the one concurrent readers broke — intermittently,
// and only on the platform where nobody was running the tests.
//
// Retried rather than worked around. The alternative is to open every reader in this tree with
// FILE_SHARE_DELETE, which is more code in more places and still cannot speak for a reader outside
// it — an editor, a backup agent, a virus scanner. A reader here holds the file for the length of
// one small read, so a bounded wait covers it.
//
// Bounded, and only for this error. A destination that is genuinely not writable fails the same way
// it always did, a fifth of a second later.
func Replace(from, to string) error {
	const budget = 200 * time.Millisecond
	wait := 500 * time.Microsecond
	deadline := time.Now().Add(budget)
	for {
		err := os.Rename(from, to)
		if err == nil || !transient(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(wait)
		if wait < 20*time.Millisecond {
			wait *= 2
		}
	}
}

// ReadFile reads a file that Write may be replacing under it, retrying the moment it cannot.
//
// ⚠ **The other half of the same platform fact.** Replace waits for readers to let go; this waits
// for Replace to finish. Windows has no atomic swap a reader can see through: while the destination
// is being replaced an opener gets ERROR_SHARING_VIOLATION, so a read that POSIX guarantees will
// see the old file or the new one simply fails here.
//
// Measured 2026-09-11: with the write side retrying, a reader polling the daemon's session record
// through 300 replacements failed 704 times — "not there or not whole" about a file that was there
// the whole time.
//
// This is not papering over a race, it is porting one. The promise being kept is the POSIX one the
// callers are written against: a reader sees a whole file. Bounded, and a path that is genuinely
// unreadable fails the same way it always did.
func ReadFile(path string) ([]byte, error) {
	const budget = 200 * time.Millisecond
	wait := 500 * time.Microsecond
	deadline := time.Now().Add(budget)
	for {
		b, err := os.ReadFile(path)
		if err == nil || !transient(err) || !time.Now().Before(deadline) {
			return b, err
		}
		time.Sleep(wait)
		if wait < 20*time.Millisecond {
			wait *= 2
		}
	}
}

// transient is how a replacement in flight shows up to whoever is on the other side of it.
//
// ⚠ **A sharing violation is not a permission error to Go.** ERROR_SHARING_VIOLATION (32) — "The
// process cannot access the file because it is being used by another process" — stays a raw errno;
// `errors.Is(err, fs.ErrPermission)` is false for it. Checking only the mapped errors made the
// retry never fire at all, which is exactly as visible as no retry: the same 704 failures, to the
// count. Named as numbers because the syscall package does not export them.
//
// ⚠ **"Not there" is NOT one of these, and putting it here cost a fifth of a second per absent
// file.** It was here defensively — a replacement surely has a window where the destination is
// gone — and that window does not exist: os.Rename on Windows is MoveFileEx with REPLACE_EXISTING,
// which swaps the name rather than unlinking it first. Measured 2026-09-11, a reader spinning on a
// file through three seconds of continuous replacement: 16,498 whole reads, 613 sharing/lock
// violations, and ENOENT **zero** times.
//
// What it did cost is on the other side. A missing file is an ORDINARY state for the records this
// package reads — daemon.List draws a socket with no record as a row saying "(unknown — no
// record)", because something is listening there either way — and every one of those rows was
// spending the whole 200ms budget waiting for a file nobody was writing. Measured: 204ms through
// ReadFile against 606µs through os.ReadFile, per absent record, on a list the TUI refreshes every
// two seconds.
const (
	errSharingViolation = syscall.Errno(32)
	errLockViolation    = syscall.Errno(33)
)

func transient(err error) bool {
	return errors.Is(err, errSharingViolation) || errors.Is(err, errLockViolation) ||
		errors.Is(err, fs.ErrPermission)
}
