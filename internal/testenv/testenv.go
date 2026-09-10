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
	"fmt"
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

// NeedExecutableBit skips the calling test where the filesystem has no executable bit to preserve.
//
// Windows has no POSIX mode. Go's Chmod there toggles one thing — the read-only attribute — and a
// file written 0755 reads back 0666. Two tests assert that a mode SURVIVES a write or a restore,
// which is a real and important promise on Unix (a restored script that lost its bit will not run)
// and is not a promise this platform can make at all.
//
// They were reporting failure for it. "This filesystem has no executable bit" and "magi dropped the
// executable bit" are different facts, and only the second one is a defect — measured 2026-09-10 as
// `mode = 666, want 755`.
//
// Probed rather than assumed from GOOS: a Unix filesystem mounted without permissions behaves the
// same way, and the question is about the filesystem the test will actually write to.
func NeedExecutableBit(t *testing.T) {
	t.Helper()
	execBitOnce.Do(func() {
		d, err := os.MkdirTemp("", "modeprobe")
		if err != nil {
			execBitErr = err
			return
		}
		defer os.RemoveAll(d)
		p := filepath.Join(d, "s")
		if execBitErr = os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); execBitErr != nil {
			return
		}
		fi, err := os.Stat(p)
		if err != nil {
			execBitErr = err
			return
		}
		if fi.Mode().Perm()&0o111 == 0 {
			execBitErr = fmt.Errorf("0755 를 썼는데 %v 로 읽힌다", fi.Mode().Perm())
		}
	})
	if execBitErr != nil {
		t.Skipf("이 파일시스템에는 지킬 실행 비트가 없다: %v", execBitErr)
	}
}

var (
	execBitOnce sync.Once
	execBitErr  error
)

// NeedRestrictivePermissions skips where the filesystem cannot keep a file to its owner.
//
// A private key and a push endpoint are written 0600 because anybody holding them can send to that
// browser. Windows has no POSIX mode: Chmod there toggles the read-only attribute and nothing else,
// so a file written 0600 reads back 0666 and is protected by whatever the directory's ACL happens
// to be — which magi does not set.
//
// ⚠ **The skip is not "this does not matter here".** It is "this platform does not make that
// promise, and nothing else in magi makes it either". A test that fails says the code is wrong; a
// test that passes would say the file is protected. Neither is true on Windows, so it says so out
// loud and stops — and the message is the record that the gap exists.
//
// Measured 2026-09-11 as `subscriptions are mode 666; the endpoint is a credential`.
func NeedRestrictivePermissions(t *testing.T) {
	t.Helper()
	permOnce.Do(func() {
		d, err := os.MkdirTemp("", "permprobe")
		if err != nil {
			permErr = err
			return
		}
		defer os.RemoveAll(d)
		p := filepath.Join(d, "secret")
		if permErr = os.WriteFile(p, []byte("x"), 0o600); permErr != nil {
			return
		}
		fi, err := os.Stat(p)
		if err != nil {
			permErr = err
			return
		}
		if got := fi.Mode().Perm(); got&0o077 != 0 {
			permErr = fmt.Errorf("0600 을 썼는데 %v 로 읽힌다", got)
		}
	})
	if permErr != nil {
		t.Skipf("이 파일시스템은 파일을 주인에게만 둘 수 없다 — 여기서는 그 약속이 성립하지 않고, "+
			"magi 도 대신할 ACL 을 세우지 않는다: %v", permErr)
	}
}

var (
	permOnce sync.Once
	permErr  error
)
