package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wsWorkspace builds a workspace holding exactly these files, all modified now.
func wsWorkspace(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The snapshot is the council's only statement about the world rather than the transcript, and it
// leads with "THE WORKSPACE RIGHT NOW". The walk does not enter vendor/, build/, dist/, target/ or
// any dotdir — for good reason, they are where a build's noise lives — and it said nothing about
// having skipped them. A workspace whose deliverable is inside one produced, verbatim:
//
//	no file in the workspace has been modified since this task started.
//
// which is a false statement about the world, handed to three members deciding whether the turn is
// finished. Every other cut magi makes is marked; this one was not.
func TestTheSnapshotSaysWhatItDidNotLookAt(t *testing.T) {
	since := time.Now().Add(-time.Hour)
	for _, dir := range []string{"vendor", "build", "dist", "target", "node_modules", ".cache"} {
		root := wsWorkspace(t, filepath.Join(dir, "deliverable.c"))
		got := worldSnapshot(root, since)
		if !strings.Contains(got, dir+"/") {
			t.Errorf("a workspace whose only modified file is under %s/ does not mention it:\n%s", dir, got)
		}
		// The claim of absence must not stand unqualified when part of the tree went unread.
		if strings.HasSuffix(strings.TrimSpace(got), "since this task started.") {
			t.Errorf("%s/: the absence claim is still unqualified:\n%s", dir, got)
		}
	}
}

// A listing that DID find files is qualified too — "here is what changed" reads as the whole of
// what changed, and a build artifact under target/ is exactly what a reader would go looking for.
func TestAPartialListingSaysItIsPartial(t *testing.T) {
	root := wsWorkspace(t, "run.py", "vendor/lib.c", "build/o.o")
	got := worldSnapshot(root, time.Now().Add(-time.Hour))
	if !strings.Contains(got, "run.py") {
		t.Fatalf("the ordinary file is missing from the listing:\n%s", got)
	}
	for _, want := range []string{"build/", "vendor/", "does not enter"} {
		if !strings.Contains(got, want) {
			t.Errorf("the listing does not say it skipped %q:\n%s", want, got)
		}
	}
}

// …and a workspace with nothing to skip says nothing about skipping. The note is the price of the
// claim being true, and charging it where there is no omission is noise in a block whose whole
// point is to be read.
func TestNothingSkippedAddsNoNote(t *testing.T) {
	since := time.Now().Add(-time.Hour)
	for _, files := range [][]string{{"run.py"}, {"src/main.go", "README.md"}, nil} {
		got := worldSnapshot(wsWorkspace(t, files...), since)
		if strings.Contains(got, "does not enter") {
			t.Errorf("%v: a workspace with no skipped tree carries the skip note:\n%s", files, got)
		}
	}
}

// The names are bounded. A workspace with a dotdir at every level would otherwise turn the note
// into the noise the skip rule exists to remove.
func TestTheSkipNoteIsBounded(t *testing.T) {
	var files []string
	for i := 0; i < snapshotSkipNameCap*3; i++ {
		files = append(files, filepath.Join(".d"+string(rune('a'+i)), "f.txt"))
	}
	got := worldSnapshot(wsWorkspace(t, files...), time.Now().Add(-time.Hour))
	if n := strings.Count(got, "/,") + strings.Count(got, "/ "); n > snapshotSkipNameCap+1 {
		t.Errorf("the note names %d trees, past the cap of %d:\n%s", n, snapshotSkipNameCap, got)
	}
	if !strings.Contains(got, "more") {
		t.Errorf("the capped name list does not say it was capped:\n%s", got)
	}
}

// The walk has a cap of its own — it stops after snapshotWalkCap entries so one finish cannot pay
// for a directory crawl — and stopping said nothing. On a workspace of 30000 modified files, 300
// directories changed, 40 were named, and the block read as a complete account of the workspace.
//
// The listing cap's own marker made it worse: "(the 40 most recent)" is a claim over everything
// that was read, and the walk cap ends the read before the tree ends, so the forty were the most
// recent of a fraction — a true-sounding sentence about the wrong population.
//
// This test writes past the cap on purpose, which is slow; it is worth the seconds because the
// number it is about is the one nothing else exercises.
func TestASnapshotThatStoppedEarlySaysSo(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 30k files")
	}
	root := t.TempDir()
	const dirs, per = 300, 100
	for d := 0; d < dirs; d++ {
		dir := filepath.Join(root, fmt.Sprintf("d%03d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < per; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.c", f)), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if dirs*per <= snapshotWalkCap {
		t.Fatalf("the tree is %d entries and the cap is %d — this does not reach it", dirs*per, snapshotWalkCap)
	}
	got := worldSnapshot(root, time.Now().Add(-time.Hour))
	if !strings.Contains(got, "part of the workspace and not all of it") {
		t.Errorf("a walk that stopped at its cap reads as a complete account:\n%s", got)
	}
	if !strings.Contains(got, "most recent of what was read") {
		t.Errorf("the listing still claims to hold the most recent of the WORKSPACE:\n%s", got)
	}
}

// A workspace under the cap gains neither note. Both are the price of a claim being true, and
// charging them where nothing was cut is noise in a block whose point is to be read.
func TestASnapshotThatReadEverythingSaysNothingExtra(t *testing.T) {
	got := worldSnapshot(wsWorkspace(t, "run.py", "src/main.go"), time.Now().Add(-time.Hour))
	for _, noise := range []string{"stopped after", "of what was read", "does not enter"} {
		if strings.Contains(got, noise) {
			t.Errorf("a complete read carries %q:\n%s", noise, got)
		}
	}
}

// The listing says what it is. It holds only files this turn CHANGED, but the heading says "THE
// WORKSPACE RIGHT NOW" and the note under it used to say "this listing is complete" — together a
// directory listing, which it has never been.
//
// Measured live (headless-terminal, 2026-08-01): the task provides /app/base_terminal.py and asks
// for a class inheriting from it. The agent read that file and imported it correctly. The snapshot
// listed only the file it wrote, and all three council members voted continue on the grounds that
// base_terminal.py does not exist — one of them citing the listing as having verified it. The
// agent then spent twelve calls chasing a file that had been there the whole time.
func TestTheSnapshotDoesNotPassItselfOffAsADirectoryListing(t *testing.T) {
	root := t.TempDir()
	// One file that was already here and is never touched, one the turn writes.
	old := filepath.Join(root, "base_terminal.py")
	if err := os.WriteFile(old, []byte("class BaseTerminal: pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, long, long); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "headless_terminal.py"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := worldSnapshot(root, time.Now().Add(-time.Minute))
	if !strings.Contains(got, "headless_terminal.py") {
		t.Fatalf("the file the turn wrote is not listed, so nothing here is under test:\n%s", got)
	}
	if strings.Contains(got, "base_terminal.py") {
		t.Fatalf("an untouched file is in a changed-file listing:\n%s", got)
	}
	// …and because it is absent, the block has to say that absence means untouched, not gone.
	if !strings.Contains(got, "not the same as not being there") {
		t.Errorf("the listing does not say a missing name may still be a present file:\n%s", got)
	}
	if strings.Contains(got, "this listing is complete") {
		t.Errorf("the listing still claims to be complete as a workspace listing:\n%s", got)
	}
}

// The skip note keeps naming what went unread — scoped to the changes, not promising a listing.
func TestTheSkipNoteStaysScopedToWhatChanged(t *testing.T) {
	root := wsWorkspace(t, "main.go", filepath.Join("vendor", "dep.go"))
	got := worldSnapshot(root, time.Now().Add(-time.Minute))
	if !strings.Contains(got, "vendor/") {
		t.Errorf("the skipped tree is no longer named:\n%s", got)
	}
	if strings.Contains(got, "this listing is complete") {
		t.Errorf("the unqualified completeness claim is back:\n%s", got)
	}
	if !strings.Contains(got, "no other changed file was left out") {
		t.Errorf("the note does not say what it is complete ABOUT:\n%s", got)
	}
}
