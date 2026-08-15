//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/update"
)

// binSource is a release source that hands back a canned version and binary bytes — enough for the
// auto-update loop to run a real RunCommit (download + commit-with-rollback) against a temp exe. The
// downloaded channel, when set, gets a tick as Download returns, so a test can start its timing
// window at the commit rather than guessing how long fork/exec of the pre-flight takes under -race.
type binSource struct {
	rel        update.Release
	bin        []byte
	downloaded chan struct{}
}

func (b binSource) Latest(context.Context) (update.Release, error) { return b.rel, nil }
func (b binSource) Download(context.Context, string) ([]byte, error) {
	if b.downloaded != nil {
		select {
		case b.downloaded <- struct{}{}:
		default:
		}
	}
	return b.bin, nil
}

var goodBin = []byte("#!/bin/sh\necho 'magi test v9'\nexit 0\n")

// runLoop starts daemonAutoUpdate and returns a join func that cancels it and WAITS for it to exit.
// The join must run before the caller's deferred global-restores: the goroutine reads latestSource /
// daemonAutoUpdateTTL on every cycle, and a test that restored them while the goroutine lived was a
// real data race — measured at ~5% failures under -race, and a cross-test hazard besides (the leaked
// goroutine kept reading globals the NEXT test reassigns).
func runLoop(t *testing.T, dir, current, exe string, running func() bool, restart func()) (cancel func()) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		daemonAutoUpdate(ctx, dir, current, exe, "sock-"+t.Name(), running, restart)
	}()
	return func() { stop(); <-done }
}

func TestDaemonAutoUpdateCommitsAndRestartsOnANewRelease(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "magi")
	if err := os.WriteFile(exe, goodBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSrc, oldTTL := latestSource, daemonAutoUpdateTTL
	latestSource = func() update.Source {
		return binSource{rel: update.Release{Version: "v99.0.0", URL: "x"}, bin: goodBin}
	}
	daemonAutoUpdateTTL = 30 * time.Millisecond
	restarted := make(chan struct{}, 1)
	join := runLoop(t, dir, "v1.0.0", exe, func() bool { return false }, func() { restarted <- struct{}{} })
	defer func() { join(); latestSource, daemonAutoUpdateTTL = oldSrc, oldTTL }()

	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("auto-update did not restart on a new release")
	}
}

// The restart holds until the daemon is idle: a build is committed while a turn runs, and the restart
// fires only once the turn ends. The window is anchored on the DOWNLOAD (the source signals it), so
// the assertion is about the idle gate and not about how fast fork/exec happens to be under -race —
// unanchored, the commit alone took longer than the old window and the test passed with the gate
// deleted.
func TestDaemonAutoUpdateWaitsForIdleBeforeRestart(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "magi")
	if err := os.WriteFile(exe, goodBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSrc, oldTTL, oldIdle := latestSource, daemonAutoUpdateTTL, daemonIdleCheckInterval
	downloaded := make(chan struct{}, 1)
	latestSource = func() update.Source {
		return binSource{rel: update.Release{Version: "v99.0.0", URL: "x"}, bin: goodBin, downloaded: downloaded}
	}
	daemonAutoUpdateTTL = 30 * time.Millisecond
	daemonIdleCheckInterval = 20 * time.Millisecond

	var busy atomic.Bool
	busy.Store(true)
	restarted := make(chan struct{}, 1)
	join := runLoop(t, dir, "v1.0.0", exe, busy.Load, func() { restarted <- struct{}{} })
	defer func() { join(); latestSource, daemonAutoUpdateTTL, daemonIdleCheckInterval = oldSrc, oldTTL, oldIdle }()

	select {
	case <-downloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("the release was never downloaded")
	}
	// From the download, the commit (a real pre-flight exec) then the idle gate. Two full seconds of
	// "still busy" is ~10x the commit cost measured under -race — a restart in this window can only
	// mean the idle gate is gone.
	select {
	case <-restarted:
		t.Fatal("restarted while a turn was in flight")
	case <-time.After(2 * time.Second):
	}
	busy.Store(false)
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("did not restart after the daemon went idle")
	}
}

// Nothing newer, nothing done: a daemon does not bounce to install the version it already runs. The
// canned release carries REAL runnable bytes — with nil bytes this test passed even without the
// IsNewer gate, because the empty download failed the pre-flight instead.
func TestDaemonAutoUpdateDoesNotRestartWhenAlreadyCurrent(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "magi")
	if err := os.WriteFile(exe, goodBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSrc, oldTTL := latestSource, daemonAutoUpdateTTL
	latestSource = func() update.Source {
		return binSource{rel: update.Release{Version: "v1.0.0", URL: "x"}, bin: goodBin}
	}
	daemonAutoUpdateTTL = 30 * time.Millisecond
	restarted := make(chan struct{}, 1)
	join := runLoop(t, dir, "v1.0.0", exe, func() bool { return false }, func() { restarted <- struct{}{} })
	defer func() { join(); latestSource, daemonAutoUpdateTTL = oldSrc, oldTTL }()

	select {
	case <-restarted:
		t.Fatal("restarted when already on the latest release")
	case <-time.After(400 * time.Millisecond):
	}
}

// A dev build never auto-updates — the loop refuses before its first check, so a locally built
// `./magi --daemon` cannot have its binary replaced by a release out from under the developer.
func TestDaemonAutoUpdateRefusesADevBuild(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "magi")
	if err := os.WriteFile(exe, goodBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSrc, oldTTL := latestSource, daemonAutoUpdateTTL
	// The release carries bytes DISTINCT from the local binary's — with identical bytes the on-disk
	// assertion below passed even when the guard was gone, because the "replacement" was a no-op.
	release := []byte("#!/bin/sh\necho 'magi test v99'\nexit 0\n")
	latestSource = func() update.Source {
		return binSource{rel: update.Release{Version: "v99.0.0", URL: "x"}, bin: release}
	}
	daemonAutoUpdateTTL = 30 * time.Millisecond
	restarted := make(chan struct{}, 1)
	// A git-describe version, not just "dev": the Makefile stamps `git describe`, so THIS is the
	// normal source build — and the first guard (bare parseSemver) read its "v0.22.2-…" prefix as a
	// release and let it through.
	join := runLoop(t, dir, "v0.22.2-13-gabc1234-dirty", exe, func() bool { return false }, func() { restarted <- struct{}{} })
	defer func() { join(); latestSource, daemonAutoUpdateTTL = oldSrc, oldTTL }()

	select {
	case <-restarted:
		t.Fatal("a source build auto-updated — a developer's local binary would be replaced by a release")
	case <-time.After(400 * time.Millisecond):
	}
	if got, _ := os.ReadFile(exe); string(got) != string(goodBin) {
		t.Error("the source-built binary on disk was replaced")
	}
}
