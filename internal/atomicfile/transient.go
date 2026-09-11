package atomicfile

import (
	"errors"
	"io/fs"
)

// transient reports whether a failed open or rename is a replacement in flight rather than an
// answer about the file.
//
// ⚠ **This predicate lives in a file with no build tag ON PURPOSE.** Everything it governs —
// Replace's retry, ReadFile's budget — is Windows-only, so the obvious home for it was
// rename_windows.go, and that is where it was. The cost of that is not visible until somebody
// reverts a decision made here: on every other platform the function does not exist, the tests
// that hold it are compiled against a ReadFile that is one line of os.ReadFile, and they pass
// without touching the thing they are about. Measured 2026-09-11 — putting fs.ErrNotExist back
// and running gofmt, vet, GOOS=windows build, GOOS=windows vet and `go test ./internal/atomicfile/`
// on a darwin checkout: all five green, about a defect that costs 200ms per absent record on the
// platform none of them run on.
//
// So the JUDGEMENT is here, where every platform compiles it and a table test can feed it errors
// directly, and only the numbers that differ stay behind a tag. Same shape as config.heldByAnother
// and internal/pathx, for the same reason.
//
// ⚠ **"Not there" is not one of these.** It was, defensively — a replacement surely has a window
// where the destination is gone — and that window does not exist: os.Rename on Windows is
// MoveFileEx with REPLACE_EXISTING, which swaps the name rather than unlinking it first. Measured
// 2026-09-11, a reader spinning on a file through three seconds of continuous replacement: 16,498
// whole reads, 613 sharing/lock violations, ENOENT zero times.
//
// What it cost is on the other side. A missing file is an ORDINARY state for the records this
// package reads — daemon.List draws a socket with no record as a row saying "(unknown — no
// record)", because something is listening there either way — and every one of those rows spent
// the whole retry budget waiting for a file nobody was writing. 204ms against 606µs, per absent
// record, on a list the TUI refreshes every two seconds.
func transient(err error) bool {
	if err == nil {
		return false
	}
	// ERROR_ACCESS_DENIED is what a destination held open by a reader answers, and it is the one
	// mapped error that means contention here.
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	for _, e := range contentionErrnos {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// contentionErrnos is the raw platform errors that mean "somebody else is in this file right now".
//
// Empty everywhere but Windows, and named as numbers there because the syscall package does not
// export them. A var rather than a build-tagged function so the predicate above stays one piece of
// code on every platform — what changes between platforms is this list, and a test can print it.
var contentionErrnos []error
