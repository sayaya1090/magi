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
// auto-update loop to run a real RunCommit (download + commit-with-rollback) against a temp exe.
type binSource struct {
	rel update.Release
	bin []byte
}

func (b binSource) Latest(context.Context) (update.Release, error)   { return b.rel, nil }
func (b binSource) Download(context.Context, string) ([]byte, error) { return b.bin, nil }

var goodBin = []byte("#!/bin/sh\necho 'magi test v9'\nexit 0\n")

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
	defer func() { latestSource, daemonAutoUpdateTTL = oldSrc, oldTTL }()

	restarted := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go daemonAutoUpdate(ctx, dir, "v1.0.0", exe, func() bool { return false }, func() { restarted <- struct{}{} })

	select {
	case <-restarted:
	case <-ctx.Done():
		t.Fatal("auto-update did not restart on a new release")
	}
}

// The restart holds until the daemon is idle: a build is committed while a turn runs, and the restart
// fires only once the turn ends.
func TestDaemonAutoUpdateWaitsForIdleBeforeRestart(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "magi")
	if err := os.WriteFile(exe, goodBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSrc, oldTTL, oldIdle := latestSource, daemonAutoUpdateTTL, daemonIdleCheckInterval
	latestSource = func() update.Source {
		return binSource{rel: update.Release{Version: "v99.0.0", URL: "x"}, bin: goodBin}
	}
	daemonAutoUpdateTTL = 30 * time.Millisecond
	daemonIdleCheckInterval = 20 * time.Millisecond
	defer func() { latestSource, daemonAutoUpdateTTL, daemonIdleCheckInterval = oldSrc, oldTTL, oldIdle }()

	var busy atomic.Bool
	busy.Store(true)
	restarted := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go daemonAutoUpdate(ctx, dir, "v1.0.0", exe, busy.Load, func() { restarted <- struct{}{} })

	select {
	case <-restarted:
		t.Fatal("restarted while a turn was in flight")
	case <-time.After(150 * time.Millisecond):
	}
	busy.Store(false)
	select {
	case <-restarted:
	case <-ctx.Done():
		t.Fatal("did not restart after the daemon went idle")
	}
}

// Nothing newer, nothing done: a daemon does not bounce to install the version it already runs.
func TestDaemonAutoUpdateDoesNotRestartWhenAlreadyCurrent(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "magi")
	if err := os.WriteFile(exe, goodBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSrc, oldTTL := latestSource, daemonAutoUpdateTTL
	latestSource = func() update.Source { return binSource{rel: update.Release{Version: "v1.0.0"}} }
	daemonAutoUpdateTTL = 30 * time.Millisecond
	defer func() { latestSource, daemonAutoUpdateTTL = oldSrc, oldTTL }()

	restarted := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	go daemonAutoUpdate(ctx, dir, "v1.0.0", exe, func() bool { return false }, func() { restarted <- struct{}{} })

	select {
	case <-restarted:
		t.Fatal("restarted when already on the latest release")
	case <-ctx.Done():
	}
}
