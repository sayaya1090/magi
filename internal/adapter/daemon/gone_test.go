package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// However this platform words it, a daemon that is not there says ErrGone.
//
// The two sentences dial produces — nothing at that path, and a socket with nothing behind it —
// were built with fmt.Errorf, so the syscall underneath was dropped and a caller could match the
// WORDS or nothing. One did: the console's "is it off?" check keyed on errno, which works on macOS
// (a leftover plain file gives ENOTSOCK and falls past both branches, keeping its wrap) and fails
// on Linux (ECONNREFUSED, replaced by a sentence). Green locally, red in CI, for a difference no
// caller should have to know about.
func TestADialThatFindsNothingIsErrGone(t *testing.T) {
	dir, err := os.MkdirTemp(shortRoot(), "magigone")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// Nothing at all: the daemon exited and took its socket with it.
	if _, err := Dial(filepath.Join(dir, "absent.sock")); !errors.Is(err, ErrGone) {
		t.Errorf("a missing socket answered %v, which is not ErrGone", err)
	}
	// Something at the path with nobody behind it: what a kill leaves.
	left := filepath.Join(dir, "left.sock")
	if werr := os.WriteFile(left, nil, 0o600); werr != nil {
		t.Fatal(werr)
	}
	if _, err := Dial(left); !errors.Is(err, ErrGone) {
		t.Errorf("a leftover socket answered %v, which is not ErrGone", err)
	}
}
