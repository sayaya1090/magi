// Package testenv keeps a test binary out of the person's own magi.
//
// A test that builds its own config directory is still steered by the environment: `SocketDir`
// prefers `MAGI_SOCKET_DIR` over whatever it was handed, by design — that is the whole reason the
// variable exists (see daemon.SocketDir). So on a machine where somebody actually runs magi, and
// therefore has it set, the fixtures quietly point at the REAL socket directory.
//
// What that costs, measured 2026-09-10 on Windows 11 with the variable set to `~\.magi`:
// `clients/web/server` failed 58 tests; with it cleared, 16. The 42 in between were reading a
// stranger's daemons. The failures do not say so — they say things like
// "nobody here is called design. There is: billing", where `billing` is a companion the person
// happens to have running and `design` is the one the test just published.
//
// Worse than the noise is the direction: a test that finds the real socket directory can also
// WRITE there, next to sockets a live companion is serving on.
//
// One place, because the rule is one rule and this tree has already paid for the same fact living
// in several files.
package testenv

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// steering is what points a magi at a particular installation, as opposed to the knobs that only
// change how one behaves (MAGI_DEBUG, MAGI_TOP_K and the rest).
//
// Only what is measured belongs here. MAGI_CONFIG_DIR and MAGI_DATA_DIR are the obvious cousins and
// were tried the same day — pointing them elsewhere changed no test's outcome, so they are not
// listed: a name in this list is a claim that leaving it alone breaks something.
var steering = []string{"MAGI_SOCKET_DIR"}

// Isolate clears the ambient settings that would point this test binary at a real magi.
//
// Call it from TestMain, before any test runs — a test that has already resolved a path cannot be
// un-steered afterwards. A test that WANTS one of these set does so itself with t.Setenv, which
// still works and is still scoped to that test.
func Isolate() {
	for _, k := range steering {
		os.Unsetenv(k)
	}
}

// NeedSymlink skips the calling test when this machine will not let it make a symlink.
//
// Windows grants the privilege only to an elevated process or with Developer Mode on, and without
// it `os.Symlink` returns "A required privilege is not held by the client". Six tests took that as
// a failure — and three of them are the jail tests, which are exactly the ones somebody looks at to
// answer "does the boundary hold". A red test that is red because the OS declined to create a
// fixture answers nothing, and it teaches the reader to skim past red.
//
// Eight sibling tests already skip on the same error, in four different wordings. The rule was
// here; it was applied to some of the copies. Measured 2026-09-10.
//
// Probed once. The answer cannot change while the binary runs, and a probe per test is a file
// created and removed for every one of them.
func NeedSymlink(t *testing.T) {
	t.Helper()
	symlinkOnce.Do(func() {
		d, err := os.MkdirTemp("", "symprobe")
		if err != nil {
			symlinkErr = err
			return
		}
		defer os.RemoveAll(d)
		symlinkErr = os.Symlink(filepath.Join(d, "target"), filepath.Join(d, "link"))
	})
	if symlinkErr != nil {
		t.Skipf("이 기계는 심볼릭 링크를 못 만든다: %v", symlinkErr)
	}
}

var (
	symlinkOnce sync.Once
	symlinkErr  error
)
