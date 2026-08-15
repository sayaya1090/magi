package identity

import (
	"os"
	"path/filepath"
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
			if _, ok := Redeem(dir, tok); ok {
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
	if label, ok := Redeem(dir, tok); !ok || label != "lee" {
		t.Fatalf("redeem: %q %v", label, ok)
	}
	if _, ok := Redeem(dir, tok); ok {
		t.Error("the same invitation was taken twice")
	}
	if Inviting(dir) {
		t.Error("the window is still open after the only invitation was spent")
	}
}

// A stale invitation is not one, and neither is a wrong secret.
func TestAnExpiredOrWrongInvitationIsRefused(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Redeem(dir, "nothing-was-minted"); ok {
		t.Error("a token nobody minted was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, TokenFile),
		[]byte(hashToken("old")+" lee 1000000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Inviting(dir) {
		t.Error("an expired invitation holds the window open")
	}
	if _, ok := Redeem(dir, "old"); ok {
		t.Error("an expired invitation was taken")
	}
}
