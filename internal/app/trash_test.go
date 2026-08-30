package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A workspace with a repository behind it needs none of this: the delete undoes from the object
// store, and a directory INSIDE a checkout is covered by that checkout.
func TestRecoverableTreeFindsTheRepositoryAbove(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if recoverableTree(deep) {
		t.Fatal("a tree with no .git anywhere above it reported history")
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !recoverableTree(deep) {
		t.Error("a directory inside a checkout is covered by that checkout")
	}
	// A worktree's .git is a FILE, and means the same thing.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, ".git"), []byte("gitdir: /elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !recoverableTree(other) {
		t.Error("a worktree keeps its .git as a file and still has history")
	}
}

// The rescue is a MOVE: the target leaves the path the command is about to remove, and is still
// there afterwards under a name the note gives out.
func TestARescuedDeleteIsMovedNotCopied(t *testing.T) {
	wd := t.TempDir()
	victim := filepath.Join(wd, "data")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(victim, "x.txt"), []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	note := a.rescueBeforeDeleting(wd, "rm -rf data", nil)
	if note == "" {
		t.Fatal("a delete with no history behind it was left to destroy the tree")
	}
	if _, err := os.Stat(victim); err == nil {
		t.Error("the target is still where the command will look for it — this was a copy, not a move")
	}
	for _, want := range []string{"no git history", trashDirName, "move it back"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note must carry %q so the model can undo it:\n%s", want, note)
		}
	}
	// And the bytes survived under the new name.
	var found string
	filepath.Walk(filepath.Join(wd, trashDirName), func(p string, fi os.FileInfo, err error) error {
		if err == nil && fi.Mode().IsRegular() && filepath.Base(p) == "x.txt" {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("the rescued tree is not in the trash")
	}
	if b, _ := os.ReadFile(found); string(b) != "keep me" {
		t.Errorf("the rescued file reads %q", b)
	}
}

// With a repository behind the tree, nothing is moved: the delete already has a way back and this
// would be work for nothing.
func TestARepositoryNeedsNoRescue(t *testing.T) {
	wd := t.TempDir()
	if err := os.Mkdir(filepath.Join(wd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wd, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	if note := a.rescueBeforeDeleting(wd, "rm -rf build", nil); note != "" {
		t.Fatalf("a checkout was given a second way back: %s", note)
	}
	if _, err := os.Stat(filepath.Join(wd, "build")); err != nil {
		t.Error("the target was moved out from under a command that could undo itself")
	}
}

// What the run itself made, and what lives in the scratch area, are the run's own output — the
// gate has always treated them that way and so does this.
func TestTheRunsOwnOutputIsNotRescued(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	mine := func(p string) bool { return strings.HasSuffix(p, "out") }
	if note := a.rescueBeforeDeleting(wd, "rm -rf out", mine); note != "" {
		t.Fatalf("the run's own output was rescued from the run: %s", note)
	}
}

// An edit's way back is a hard link: the old contents survive because the write replaces the file
// atomically, and holding a second name for that inode costs no disk at all.
func TestAnEditKeepsWhatItReplaces(t *testing.T) {
	wd := t.TempDir()
	f := filepath.Join(wd, "note.md")
	if err := os.WriteFile(f, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	turn := time.Now()
	where, kept, err := keepBeforeEditing(wd, "note.md", turn, nil)
	if err != nil || !kept {
		t.Fatalf("the previous contents were not kept: %v %v", kept, err)
	}
	// The write that follows: atomic, so the old inode lives on under the kept name.
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, f); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != "after" {
		t.Fatalf("the edit did not land: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, where)); string(b) != "before" {
		t.Errorf("what the edit replaced is gone: %q", b)
	}
	// Once per file per turn: a second edit in the same turn keeps the FIRST state.
	if _, again, _ := keepBeforeEditing(wd, "note.md", turn, nil); again {
		t.Error("a second edit in one turn overwrote the state the turn started from")
	}
}

// The sweep is always one turn behind itself, so the batch a person is most likely to want back —
// the one the turn that just ended made — is still there.
func TestTheSweepKeepsTheNewestBatches(t *testing.T) {
	wd := t.TempDir()
	trash := filepath.Join(wd, trashDirName)
	names := []string{"20260101-000000.000", "20260102-000000.000", "20260103-000000.000", "20260104-000000.000"}
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(trash, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, n := range names[:2] {
		if err := os.Chtimes(filepath.Join(trash, n), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if got := sweepTrash(wd); got != 2 {
		t.Fatalf("the sweep took %d batches, want the two oldest", got)
	}
	for _, n := range names[2:] {
		if _, err := os.Stat(filepath.Join(trash, n)); err != nil {
			t.Errorf("%s was swept; the two newest are the way back that must survive", n)
		}
	}
}

// A file this turn made has no "before the turn" to keep, and neither the delete nor the edit
// path pretends otherwise: what the run created is the run's own output.
func TestWhatThisTurnMadeIsNotHeldOnTo(t *testing.T) {
	wd := t.TempDir()
	f := filepath.Join(wd, "fresh.txt")
	if err := os.WriteFile(f, []byte("made here"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := func(p string) bool { return strings.HasSuffix(p, "fresh.txt") }
	if _, kept, _ := keepBeforeEditing(wd, "fresh.txt", time.Now(), mine); kept {
		t.Error("the first draft of a file this turn created was kept as if it predated the turn")
	}
	// And without that knowledge it IS kept, so the exemption is what decided it.
	if _, kept, _ := keepBeforeEditing(wd, "fresh.txt", time.Now(), nil); !kept {
		t.Error("a file nobody claims was left unheld")
	}
}
