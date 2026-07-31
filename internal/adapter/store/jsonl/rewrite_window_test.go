package jsonl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Rewriting a log used to be two renames: the original moved aside to the archive name, then the
// replacement moved in. Between them the session has no file at all — and that state is silent.
// Measured on the state itself: a fresh Read returns 0 events with a nil error and ListSessions
// returns 0 sessions with a nil error, so a session whose entire history is sitting one filename
// away reads as one that never existed.
//
// A process killed at that instant is not exotic here. Compaction runs mid-turn, and mid-turn is
// when a wall clock or an operator ends a run.
func TestTheRewriteNeverLeavesTheSessionWithoutAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	tmp := path + ".tmp"
	archive := path + ".archive"
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("rewritten\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The property the two-rename sequence could not have: while the archive already exists, the
	// path is STILL the original. Only one move remains, and it is the atomic replace.
	if err := os.Remove(archive); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Link(path, archive); err != nil {
		t.Skipf("this filesystem has no hard links: %v", err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "original\n" {
		t.Fatalf("the path stopped holding the original once the archive existed: %q %v", b, err)
	}
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}

	if err := archiveThenReplace(path, tmp, archive); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "rewritten\n" {
		t.Errorf("the path does not hold the rewrite: %q %v", got, err)
	}
	kept, err := os.ReadFile(archive)
	if err != nil || string(kept) != "original\n" {
		t.Errorf("the archive does not hold the original: %q %v", kept, err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("the temporary file was left behind: %v", err)
	}
}

// An archive from a previous rewrite is replaced rather than blocking the new one. Link refuses an
// existing target, and a compaction that failed because the last one had already run would be a
// worse outcome than an overwritten archive — which is what the name has always meant.
func TestASecondRewriteReplacesTheArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	archive := path + ".archive"
	if err := os.WriteFile(archive, []byte("older\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("newest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := archiveThenReplace(path, path+".tmp", archive); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(archive)
	if err != nil || strings.TrimSpace(string(b)) != "current" {
		t.Errorf("the archive holds %q, want the log this rewrite replaced", b)
	}
}
