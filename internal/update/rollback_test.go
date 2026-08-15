//go:build !windows

package update

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Fake "binaries": shell scripts that respond to the --version pre-flight. Runnable magi stand-ins.
var (
	goodBinary = []byte("#!/bin/sh\necho 'magi test v9'\nexit 0\n")
	badBinary  = []byte("#!/bin/sh\nexit 1\n") // execs, but --version fails
	hangBinary = []byte("#!/bin/sh\nsleep 30\n")
)

func writeExec(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAcceptsARunnableBinaryAndRejectsBadOnes(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	bad := filepath.Join(dir, "bad")
	hang := filepath.Join(dir, "hang")
	writeExec(t, good, goodBinary)
	writeExec(t, bad, badBinary)
	writeExec(t, hang, hangBinary)

	if err := Verify(good); err != nil {
		t.Errorf("a runnable binary failed pre-flight: %v", err)
	}
	if err := Verify(bad); err == nil {
		t.Error("a binary that fails --version passed pre-flight")
	}
	if err := Verify(filepath.Join(dir, "nonexistent")); err == nil {
		t.Error("a missing binary passed pre-flight")
	}

	// A hang is caught by the timeout, not left to stall the update — even when the hung binary left
	// a child holding the output pipe (WaitDelay bounds that).
	oldT, oldW := verifyTimeout, verifyWaitDelay
	verifyTimeout, verifyWaitDelay = 200*time.Millisecond, 200*time.Millisecond
	defer func() { verifyTimeout, verifyWaitDelay = oldT, oldW }()
	start := time.Now()
	if err := Verify(hang); err == nil {
		t.Error("a binary that hangs on start passed pre-flight")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("the hang pre-flight took %s; it should fire around the timeout, not wait for the sleep", d)
	}
}

// Commit must leave the on-disk binary as one that passed pre-flight: a bad new build is rolled back
// and the previous one restored, so the daemon never restarts into a binary that will not come up.
func TestCommitRollsBackABadBuildAndKeepsAGoodOne(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	writeExec(t, target, goodBinary) // the currently-running, known-good binary

	// A bad new build: Commit applies it, the pre-flight fails, and the good one comes back.
	if err := Commit(badBinary, target); err == nil {
		t.Error("Commit accepted a binary that fails pre-flight")
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, goodBinary) {
		t.Errorf("after a rolled-back update the on-disk binary is not the restored good one:\n%s", got)
	}
	if err := Verify(target); err != nil {
		t.Errorf("the restored binary does not run: %v", err)
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Error("the saved-previous copy was left behind after rollback")
	}

	// A good new build: Commit takes it, and nothing is left behind.
	newGood := []byte("#!/bin/sh\necho 'magi test v10'\nexit 0\n")
	if err := Commit(newGood, target); err != nil {
		t.Errorf("Commit rejected a good build: %v", err)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, newGood) {
		t.Error("after a good update the on-disk binary is not the new one")
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Error("the saved-previous copy was left behind after a successful update")
	}
}

// KeepPrevious/restore round-trips the exact bytes, so a rollback is byte-for-byte the old binary.
func TestKeepPreviousRestoresTheExactBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	writeExec(t, target, goodBinary)

	restore, discard, err := KeepPrevious(target)
	if err != nil {
		t.Fatal(err)
	}
	// Overwrite with something else, then restore.
	writeExec(t, target, badBinary)
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, goodBinary) {
		t.Error("restore did not bring back the exact previous binary")
	}
	discard()
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Error("discard did not remove the saved copy")
	}
}
