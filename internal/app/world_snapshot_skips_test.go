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
