package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/update"
)

type fakeSource struct {
	rel update.Release
	err error
}

func (f fakeSource) Latest(context.Context) (update.Release, error)   { return f.rel, f.err }
func (f fakeSource) Download(context.Context, string) ([]byte, error) { return nil, nil }

// The bench-safety invariant: the startup check must NOT fire when headless
// (-p) or when stdout is not a TTY (pipe/CI/benchmark), regardless of opt-out.
func TestShouldCheckUpdatesGate(t *testing.T) {
	cases := []struct {
		headless, isTTY, optOut, want bool
	}{
		{false, true, false, true},   // interactive TTY, not opted out → check
		{true, true, false, false},   // headless (-p) → never
		{false, false, false, false}, // non-TTY (pipe/bench) → never
		{true, false, false, false},  // headless + non-TTY → never
		{false, true, true, false},   // opted out → never
	}
	for _, c := range cases {
		if got := shouldCheckUpdates(c.headless, c.isTTY, c.optOut); got != c.want {
			t.Errorf("shouldCheckUpdates(headless=%v,tty=%v,optOut=%v) = %v, want %v",
				c.headless, c.isTTY, c.optOut, got, c.want)
		}
	}
}

func TestUpdateCheckDue(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, ".update-check")
	now := time.Now()

	// Missing stamp → due.
	if !updateCheckDue(stamp, updateCheckTTL, now) {
		t.Fatal("missing stamp should be due")
	}
	if err := os.WriteFile(stamp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Just written → not due.
	if updateCheckDue(stamp, updateCheckTTL, now) {
		t.Fatal("fresh stamp should not be due")
	}
	// Past the TTL → due again.
	if !updateCheckDue(stamp, updateCheckTTL, now.Add(25*time.Hour)) {
		t.Fatal("stale stamp should be due")
	}
}

// swapSeams installs test doubles and returns a restore func.
func swapSeams(t *testing.T, rel update.Release, err error, install update.Result, installErr error) *int {
	t.Helper()
	calls := 0
	origSrc, origInstall, origWin := latestSource, forceInstallFn, forceAbortWindow
	latestSource = func() update.Source { return fakeSource{rel: rel, err: err} }
	forceInstallFn = func(context.Context, update.Source, string, string) (update.Result, error) {
		calls++
		return install, installErr
	}
	forceAbortWindow = 0
	t.Cleanup(func() { latestSource, forceInstallFn, forceAbortWindow = origSrc, origInstall, origWin })
	return &calls
}

// A patch bump only notifies: banner printed, no install, keep running (false).
func TestMaybeUpdatePatchNotifies(t *testing.T) {
	calls := swapSeams(t, update.Release{Version: "1.2.4"}, nil, update.Result{}, nil)
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), t.TempDir(), "1.2.3", "/x/magi", &out); installed {
		t.Fatal("patch bump must not install")
	}
	if *calls != 0 {
		t.Fatalf("forceInstall called %d times on a patch bump, want 0", *calls)
	}
	if !strings.Contains(out.String(), "1.2.4 is available") {
		t.Fatalf("expected notify banner, got %q", out.String())
	}
}

// A minor bump is required: it installs and signals the caller to exit (true).
func TestMaybeUpdateMinorForces(t *testing.T) {
	calls := swapSeams(t, update.Release{Version: "1.3.0"}, nil, update.Result{Updated: true, From: "1.2.3", To: "1.3.0"}, nil)
	var out bytes.Buffer
	installed := maybeUpdateOnStartup(context.Background(), t.TempDir(), "1.2.3", "/x/magi", &out)
	if !installed {
		t.Fatal("minor bump must install and return true (caller exits)")
	}
	if *calls != 1 {
		t.Fatalf("forceInstall called %d times, want 1", *calls)
	}
	if !strings.Contains(out.String(), "required update") {
		t.Fatalf("expected required-update notice, got %q", out.String())
	}
}

// A failed install must not wedge startup: swallow the error, keep running (false).
func TestMaybeUpdateForceFailureContinues(t *testing.T) {
	swapSeams(t, update.Release{Version: "2.0.0"}, nil, update.Result{}, context.DeadlineExceeded)
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), t.TempDir(), "1.2.3", "/x/magi", &out); installed {
		t.Fatal("failed install must not report installed")
	}
	if !strings.Contains(out.String(), "auto-update failed") {
		t.Fatalf("expected failure notice, got %q", out.String())
	}
}

// A fresh stamp short-circuits: no source call, nothing printed.
func TestMaybeUpdateRespectsCache(t *testing.T) {
	calls := swapSeams(t, update.Release{Version: "9.9.9"}, nil, update.Result{}, nil)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".update-check"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), dir, "1.2.3", "/x/magi", &out); installed {
		t.Fatal("cached check should be a no-op")
	}
	if *calls != 0 || out.Len() != 0 {
		t.Fatalf("cached check should not act: calls=%d out=%q", *calls, out.String())
	}
}

// An offline/errored source is swallowed and stamps the attempt (no hammering).
func TestMaybeUpdateOfflineStampsAndContinues(t *testing.T) {
	swapSeams(t, update.Release{}, context.DeadlineExceeded, update.Result{}, nil)
	dir := t.TempDir()
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), dir, "1.2.3", "/x/magi", &out); installed {
		t.Fatal("offline check must not install")
	}
	if _, err := os.Stat(filepath.Join(dir, ".update-check")); err != nil {
		t.Fatalf("offline check should still stamp the attempt: %v", err)
	}
}
