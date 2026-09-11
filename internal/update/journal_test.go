// 이 시험들은 진짜 셸 스크립트를 바이너리 자리에 세워 프리플라이트와 기동을 재므로 유닉스에서만
// 컴파일된다 — rollback_test.go 와 같은 이유이고, 그 파일의 writeExec 를 같이 쓴다. 윈도우의
// 교체는 모양이 달라(도는 이미지를 못 덮는다) 재는 자리가 그 플랫폼에 따로 있어야 한다.
//go:build !windows

package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// install writes a target binary with a saved previous copy beside it and a pending transaction, the
// state Commit leaves behind: the candidate is in place and the build it replaced is recoverable.
func install(t *testing.T, dir, from, to string) string {
	t.Helper()
	target := filepath.Join(dir, "magi")
	writeExec(t, target, []byte("#!/bin/sh\necho 'magi "+to+"'\n"))
	if err := os.WriteFile(target+".prev", []byte("#!/bin/sh\necho 'magi "+from+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Began(target, Versions{From: from, To: to}); err != nil {
		t.Fatal(err)
	}
	return target
}

// The generation the update restarted into is watched, not trusted: it holds the previous build until
// it has been up for the stable window, and only Confirm throws that away.
func TestFirstGenerationIsWatchedAndConfirmClosesIt(t *testing.T) {
	target := install(t, t.TempDir(), "v1.0.0", "v2.0.0")

	got, err := Resume(target, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Watching || got.RolledBack {
		t.Fatalf("the first start on a candidate should be watched, got %+v", got)
	}
	if _, err := os.Stat(target + ".prev"); err != nil {
		t.Error("the previous build was dropped before the window elapsed")
	}

	if err := Confirm(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Error("Confirm left the previous build behind")
	}
	if _, err := os.Stat(journalOf(target)); !os.IsNotExist(err) {
		t.Error("Confirm left the transaction open")
	}
	// And a start after that finds nothing to do.
	again, err := Resume(target, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if again.Watching || again.RolledBack {
		t.Errorf("a confirmed install still has a transaction: %+v", again)
	}
}

// The defect this file exists for: a build that answers --version, comes up, and does NOT stay up.
// The second start on the same candidate means the first one never reached Confirm.
func TestASecondStartOnTheSameCandidateRollsBack(t *testing.T) {
	dir := t.TempDir()
	target := install(t, dir, "v1.0.0", "v2.0.0")
	prevBytes, err := os.ReadFile(target + ".prev")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Resume(target, "v2.0.0"); err != nil { // came up…
		t.Fatal(err)
	} // …and died before the window: no Confirm.

	got, err := Resume(target, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !got.RolledBack || got.Watching {
		t.Fatalf("a candidate that did not survive should be rolled back, got %+v", got)
	}
	if got.From != "v1.0.0" || got.To != "v2.0.0" {
		t.Errorf("the rollback names the wrong builds: %+v", got)
	}
	on, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(on, prevBytes) {
		t.Errorf("the binary on disk is not the restored previous build:\n%s", on)
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Error("the saved copy was left behind after it was restored")
	}
	if r := Refused(target); r != "v2.0.0" {
		t.Errorf("the rolled-back build was not recorded as refused: %q", r)
	}
}

// A refused build is not taken again by the automatic path, and an explicit retry clears that.
func TestTheAutomaticPathSkipsARefusedBuildUntilRetry(t *testing.T) {
	dir := t.TempDir()
	target := install(t, dir, "v1.0.0", "v2.0.0")
	if _, err := Resume(target, "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := Resume(target, "v2.0.0"); err != nil { // rolls back, refuses v2.0.0
		t.Fatal(err)
	}

	downloads := 0
	src := countingSource{version: "v2.0.0", body: []byte("whatever"), downloads: &downloads}
	res, err := RunCommit(context.Background(), src, "v1.0.0", target)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated {
		t.Error("the automatic path re-applied a build this install had already rolled back")
	}
	if downloads != 0 {
		t.Errorf("a refused build was downloaded again %d time(s) — the skip must come before the network", downloads)
	}
	if res.Skipped == "" {
		t.Error("the skip was silent; the reason has to travel with it")
	}

	if err := Retry(target); err != nil {
		t.Fatal(err)
	}
	if r := Refused(target); r != "" {
		t.Errorf("an explicit retry did not clear the refusal: %q", r)
	}
}

// Stopping a daemon on purpose a few seconds after an update is an ordinary thing to do, and it is
// not the same fact as a build falling over. Without this the next start would roll a good build back.
func TestACleanStopIsNotAFallenOverBuild(t *testing.T) {
	target := install(t, t.TempDir(), "v1.0.0", "v2.0.0")
	if _, err := Resume(target, "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := LeftCleanly(target); err != nil {
		t.Fatal(err)
	}
	got, err := Resume(target, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.RolledBack {
		t.Error("a deliberate shutdown was read as a build that could not stay up")
	}
	if !got.Watching {
		t.Errorf("the start after a clean stop should be watched again, got %+v", got)
	}
}

// Somebody starting an older binary by hand says nothing about another install's transaction.
func TestAnotherVersionRunningLeavesTheTransactionAlone(t *testing.T) {
	target := install(t, t.TempDir(), "v1.0.0", "v2.0.0")
	got, err := Resume(target, "v1.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Watching || got.RolledBack {
		t.Fatalf("a different running version moved someone else's transaction: %+v", got)
	}
	if _, err := os.Stat(target + ".prev"); err != nil {
		t.Error("the previous build was dropped by an unrelated process")
	}
	// The record is untouched, so the real candidate still gets its one watched start.
	after, err := Resume(target, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !after.Watching {
		t.Errorf("the candidate lost its watched start: %+v", after)
	}
}

// A rollback with nothing to roll back to says so rather than reporting success.
func TestRollbackWithoutASavedBuildIsAnError(t *testing.T) {
	target := install(t, t.TempDir(), "v1.0.0", "v2.0.0")
	if _, err := Resume(target, "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target + ".prev"); err != nil {
		t.Fatal(err)
	}
	got, err := Resume(target, "v2.0.0")
	if err == nil {
		t.Fatal("a rollback with no saved build reported success")
	}
	if got.RolledBack {
		t.Error("it claimed to have rolled back with nothing to roll back to")
	}
}

// The window is the number the clients use for the same question, not a second one that can drift.
func TestStableWindowMatchesTheLaunchPolicy(t *testing.T) {
	if StableWindow.Seconds() != 60 {
		t.Errorf("StableWindow is %s; CLIENT_LIFECYCLE §5 and §9.3 both say 60s", StableWindow)
	}
}

// countingSource is fakeSource with a download tally: the refusal has to be read BEFORE the network,
// and only counting proves it.
type countingSource struct {
	version   string
	body      []byte
	downloads *int
}

func (c countingSource) Latest(context.Context) (Release, error) {
	return Release{Version: c.version, URL: "http://example.invalid/magi"}, nil
}

func (c countingSource) Download(context.Context, string) ([]byte, error) {
	*c.downloads++
	return c.body, nil
}

// Two daemons share one binary — the ordinary shape on a machine with several companions. Without an
// OS lock each holds its own process mutex, sees nothing of the other, and one saves the OTHER's new
// build as "previous".
func TestOnlyOneProcessReplacesAnInstall(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	writeExec(t, target, goodBinary)

	// Somebody else holds the install lock. Taken the way another process would take it — a separate
	// open of the same file — because that is what the lock has to survive.
	held, ok := holdInstall(target)
	if !ok {
		t.Fatal("could not take the install lock at all")
	}

	err := Commit([]byte("#!/bin/sh\necho 'magi other'\n"), target, Versions{From: "v1.0.0", To: "v2.0.0"})
	if !errors.Is(err, ErrInstallBusy) {
		t.Fatalf("a second replacement went ahead while another process held the install: %v", err)
	}
	on, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, goodBinary) {
		t.Error("the binary was replaced by a process that did not hold the lock")
	}
	if _, serr := os.Stat(target + ".prev"); serr == nil {
		t.Error("the blocked process still saved a previous copy — that is the file the two would fight over")
	}

	// And once it is free, the same call goes through.
	held()
	if err := Commit([]byte("#!/bin/sh\necho 'magi other'\n"), target, Versions{From: "v1.0.0", To: "v2.0.0"}); err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
}

// A replacement interrupted BEFORE it was recorded: a `.prev` and no journal. The binary on disk is
// either the original or one that never finished its pre-flight, and nothing says which.
func TestAnInterruptedReplacementIsPutBack(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	writeExec(t, target, []byte("#!/bin/sh\necho 'magi half-installed'\n"))
	previous := []byte("#!/bin/sh\necho 'magi v1'\n")
	if err := os.WriteFile(target+".prev", previous, 0o755); err != nil {
		t.Fatal(err)
	}

	put, err := Salvage(target)
	if err != nil {
		t.Fatal(err)
	}
	if put == "" {
		t.Fatal("an interrupted replacement was left in place")
	}
	on, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, previous) {
		t.Errorf("the previous build was not put back:\n%s", on)
	}
	// Nothing to do the second time, and a clean install is left alone.
	if again, aerr := Salvage(target); aerr != nil || again != "" {
		t.Errorf("salvage ran twice on one interruption: (%q, %v)", again, aerr)
	}
}

// A RECORDED transaction is not an interrupted replacement, and Salvage must keep its hands off it —
// the build being watched is exactly a `.prev` beside a journal, and restoring it would undo every
// update one start after it landed.
func TestSalvageLeavesAWatchedUpdateAlone(t *testing.T) {
	dir := t.TempDir()
	target := install(t, dir, "v1.0.0", "v2.0.0")
	candidate, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	put, serr := Salvage(target)
	if serr != nil || put != "" {
		t.Fatalf("salvage took over a recorded transaction: (%q, %v)", put, serr)
	}
	on, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, candidate) {
		t.Error("a watched update was rolled back by the interrupted-replacement path")
	}
}

// Confirm has two destructive steps, and this asks about the ORDER it does them in — by stopping
// between them for real.
//
// Interrupted there, the disk shows a `.prev` beside a pending record, which is ALSO what a build
// still being watched looks like. The stage is what tells them apart. Write the record second
// instead of first and the interruption instead leaves a `.prev` with NO record — indistinguishable
// from a replacement that died before it was recorded, whose recovery is to put the backup back.
// That would undo a build which had already proven itself.
func TestAConfirmInterruptedMidWayIsFinishedNotUndone(t *testing.T) {
	dir := t.TempDir()
	target := install(t, dir, "v1.0.0", "v2.0.0")
	candidate, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, rerr := Resume(target, "v2.0.0"); rerr != nil { // came up, and lasted its window
		t.Fatal(rerr)
	}

	// The machine dies exactly as the backup is being dropped.
	was := removePrev
	removePrev = func(string) error { return errors.New("power cut") }
	cerr := Confirm(target)
	removePrev = was
	if cerr == nil {
		t.Fatal("the interruption did not happen, so nothing below is about one")
	}

	// The next start, in the order the daemon does it: salvage an unrecorded replacement, then
	// resume a recorded one.
	if put, serr := Salvage(target); serr != nil || put != "" {
		t.Fatalf("an interrupted confirm was taken for an interrupted replacement — %q was put back "+
			"over a build that had already proven itself", put)
	}
	got, rerr := Resume(target, "v2.0.0")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if got.RolledBack {
		t.Fatal("an interrupted confirm was read as a build that could not stay up")
	}
	on, oerr := os.ReadFile(target)
	if oerr != nil {
		t.Fatal(oerr)
	}
	if !bytes.Equal(on, candidate) {
		t.Error("the confirmed build was replaced by the one it had already beaten")
	}
	if _, serr := os.Stat(target + ".prev"); !os.IsNotExist(serr) {
		t.Error("the confirm was not finished — the backup is still there")
	}
	if _, serr := os.Stat(journalOf(target)); !os.IsNotExist(serr) {
		t.Error("the transaction is still open after being finished")
	}
}
