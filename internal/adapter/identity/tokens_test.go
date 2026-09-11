package identity

import (
	"os"
	"path/filepath"

	"github.com/sayaya1090/magi/internal/testenv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// A one-use invitation survives exactly one of many concurrent redeems. The TLS server handles
// joins in parallel, so two bearing the same token once both read it present and both wrote the file
// without it — admitting two keys. redeemMu serializes the read-modify-write. Without it this counts
// more than one success.
func TestAnInvitationSurvivesExactlyOneOfManyConcurrentRedeems(t *testing.T) {
	dir := t.TempDir()
	tok, err := Mint(dir, "lee")
	if err != nil {
		t.Fatal(err)
	}
	const n = 32
	var wg sync.WaitGroup
	var success atomic.Int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok, _ := Redeem(dir, tok); ok {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := success.Load(); got != 1 {
		t.Errorf("a one-use invitation was redeemed %d times concurrently, want exactly 1", got)
	}
}

// An invitation is a secret that stands briefly and is spent once.
func TestAnInvitationIsSpentOnce(t *testing.T) {
	dir := t.TempDir()
	if Inviting(dir) {
		t.Fatal("a machine with no invitations is inviting")
	}
	tok, err := Mint(dir, "lee")
	if err != nil {
		t.Fatal(err)
	}
	if !Inviting(dir) {
		t.Error("the window did not open")
	}
	// What is on disk is a digest: this file is a file of passwords otherwise, and it is read by
	// anything that can read the config directory.
	raw, err := os.ReadFile(filepath.Join(dir, TokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), tok) {
		t.Error("the invitation itself is written down")
	}
	if label, ok, _ := Redeem(dir, tok); !ok || label != "lee" {
		t.Fatalf("redeem: %q %v", label, ok)
	}
	if _, ok, _ := Redeem(dir, tok); ok {
		t.Error("the same invitation was taken twice")
	}
	if Inviting(dir) {
		t.Error("the window is still open after the only invitation was spent")
	}
}

// A stale invitation is not one, and neither is a wrong secret.
func TestAnExpiredOrWrongInvitationIsRefused(t *testing.T) {
	dir := t.TempDir()
	if _, ok, _ := Redeem(dir, "nothing-was-minted"); ok {
		t.Error("a token nobody minted was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, TokenFile),
		[]byte(hashToken("old")+" lee 1000000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Inviting(dir) {
		t.Error("an expired invitation holds the window open")
	}
	if _, ok, _ := Redeem(dir, "old"); ok {
		t.Error("an expired invitation was taken")
	}
}

// An invitation minted while somebody else is joining survives. Redeem rewrites the whole file;
// Mint appends to it — and the mint used to sit outside the lock the redeem takes, so a token a
// person had just been handed could be erased before they could use it (measured: 4 in 60).
func TestAnInvitationMintedDuringAJoinIsNotLost(t *testing.T) {
	for i := 0; i < 40; i++ {
		dir := t.TempDir()
		old, err := Mint(dir, "first")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var fresh string
		var merr error
		wg.Add(2)
		go func() { defer wg.Done(); Redeem(dir, old) }()
		go func() { defer wg.Done(); fresh, merr = Mint(dir, "second") }()
		wg.Wait()
		if merr != nil {
			t.Fatal(merr)
		}
		if _, ok, _ := Redeem(dir, fresh); !ok {
			t.Fatalf("round %d: an invitation minted during a join was lost", i)
		}
	}
}

// A file that cannot be written says so, instead of reporting the invitation as never open.
//
// ⚠ **The precondition is the whole test, and it used to be a POSIX sentence.** `os.Chmod(dir,
// 0o500)` makes a directory unwritable on Unix and does nothing of the kind on Windows, where that
// attribute does not stop anything being created, replaced or deleted inside. So the write
// succeeded, Redeem spent the token correctly, and the assertion below fired — a security test
// announcing that an unspendable invitation admitted somebody, about an invitation that was
// perfectly spendable. Measured 2026-09-11 as `"lee" true`.
//
// testenv.BlockWrites states the precondition in whatever way this machine actually honours, and
// skips out loud where it cannot. That matters more here than in most places: this is the one
// check standing between a failed write and a door that opens anyway, and a skip says so while a
// false red teaches the reader to skim past it.
func TestAnUnspendableInvitationReportsWhy(t *testing.T) {
	dir := t.TempDir()
	tok, err := Mint(dir, "lee")
	if err != nil {
		t.Fatal(err)
	}
	testenv.BlockWrites(t, dir, filepath.Join(dir, TokenFile))

	label, ok, rerr := Redeem(dir, tok)
	if ok || label != "" {
		t.Fatalf("a token that could not be spent must not admit anybody: %q %v", label, ok)
	}
	if rerr == nil {
		t.Fatal("the write failure was reported as an ordinary refusal")
	}
}
