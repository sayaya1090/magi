package app

import (
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
