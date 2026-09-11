package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/testenv"
)

// An identity is made once and kept.
func TestAnIdentityIsMadeOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	first, err := Load(dir, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Fingerprint(), "SHA256:") {
		t.Errorf("fingerprint = %q", first.Fingerprint())
	}
	// Whole, not truncated: it exists to be compared by eye over a channel nobody controls, and a
	// prefix is something an attacker can grind for.
	if len(first.Fingerprint()) < 40 {
		t.Errorf("fingerprint is short enough to grind: %q", first.Fingerprint())
	}
	again, err := Load(dir, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if again.Fingerprint() != first.Fingerprint() {
		t.Error("the identity changed between runs, so every admission of it expired for no reason")
	}
}

// The private half is not readable by anybody else.
//
// ⚠ **Its own test, because the guard on it ends whatever test it stands in.** Windows has no
// POSIX mode — Chmod there toggles the read-only attribute and nothing else, so a key written 0600
// reads back 0666 and is protected by whatever ACL the directory happens to carry, which magi does
// not set. testenv.NeedRestrictivePermissions says that out loud and skips; standing inside
// TestAnIdentityIsMadeOnceAndKept it took the identity-is-stable checks down with it, and a whole
// test reporting SKIP is how coverage disappears without anybody noticing. Measured 2026-09-11:
// that test was red here for `the key is mode -rw-rw-rw-`, which is a fact about the filesystem
// and not about magi.
func TestThePrivateKeyIsNotReadableByAnybodyElse(t *testing.T) {
	testenv.NeedRestrictivePermissions(t)
	dir := t.TempDir()
	if _, err := Load(dir, "laptop"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("the key is mode %v — anybody on this machine can sign as this daemon", fi.Mode().Perm())
	}
}

// Two machines sharing one config directory are two identities, not one.
//
// A shared config dir is a thing this tree already warns about, and two daemons holding one key
// are one party as far as anybody admitting them can tell — so admitting either admits both,
// silently. Regenerating turns that into a new fingerprint somebody has to admit.
func TestAKeyDoesNotFollowAConfigDirectoryToAnotherMachine(t *testing.T) {
	dir := t.TempDir()
	mine, err := Load(dir, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := Load(dir, "buildbox")
	if err != nil {
		t.Fatal(err)
	}
	if theirs.Fingerprint() == mine.Fingerprint() {
		t.Error("the second machine presents the first machine's identity")
	}
}
