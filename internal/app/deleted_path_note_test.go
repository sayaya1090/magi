package app

import (
	"os"
	"path/filepath"
	"testing"
)

// magi files an absent file and an empty one under the same content hash, so a command that
// DELETED something looks, to the content history, exactly like one that put a file back to an
// earlier state. Observed live (fix-ocaml-gc, 2026-07-30): a scratch file written at 02:12:26 with
// `sed … > /tmp/sweep_function.txt` and removed at 02:16:47 with `rm -f` came back carrying
//
//	[self-edit check] /tmp/sweep_function.txt: note: this edit restored a content state this file
//	already had earlier this turn — if reverting your own earlier change was intentional, ignore this.
//
// Nothing was restored; a temp file was cleaned up. The distinction is a stat away, and magi
// already makes that stat when it reads the file.
func TestPathExistsSeparatesAbsentFromEmpty(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "gone.txt")

	// Both read as "" through the content path…
	if c, _ := readForChange(dir, empty); c != "" {
		t.Fatalf("an empty file reads as empty, got %q", c)
	}
	if c, _ := readForChange(dir, gone); c != "" {
		t.Fatalf("a missing file reads as empty too, got %q", c)
	}
	// …and only the stat tells them apart.
	if !pathExists(dir, empty) {
		t.Error("an empty file exists")
	}
	if pathExists(dir, gone) {
		t.Error("a missing file does not")
	}
	// Relative and absolute paths resolve the same way.
	if !pathExists(dir, "empty.txt") {
		t.Error("a relative path resolves against the workdir")
	}
	// A directory exists, which is what keeps a `rm -rf dir` that failed from reading as a deletion.
	if !pathExists(dir, ".") {
		t.Error("a directory exists")
	}
}
