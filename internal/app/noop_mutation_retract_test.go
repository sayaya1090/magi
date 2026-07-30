package app

import (
	"os"
	"path/filepath"
	"testing"
)

// A bash mutation bumps the epoch and restarts the no-progress window, and retractProgress exists
// to take that back when the command turns out to have moved nothing. It needs compared > 0 — at
// least one destination magi could weigh — and every path that was empty on both sides used to be
// skipped before it got there.
//
// So a command whose destinations were all already gone bought a fresh window that nothing could
// reclaim. Observed live (cancel-async-tasks, 2026-07-30): seven
// `rm -f /app/test_cleanup.py /app/test_real_signal.py /app/test_sigint.py && ls …` calls after the
// files were already deleted, in two command-text variants, each variant restarting the window.
//
// "Empty on both sides" hides four shapes, and the stat taken with the snapshot separates them.
func TestEmptyOnBothSidesIsWeighedByExistence(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	absent := filepath.Join(dir, "gone.txt")
	empty := mk("empty.txt", "")
	full := mk("full.txt", "x")

	// The two shapes that changed nothing: absent stayed absent, empty stayed empty.
	if pathExists(dir, absent) {
		t.Fatal("setup: gone.txt must not exist")
	}
	if !pathExists(dir, empty) {
		t.Fatal("setup: empty.txt must exist")
	}
	// A creation and a deletion also read as empty on both sides, and must NOT count as unchanged.
	created := filepath.Join(dir, "created.txt")
	if pathExists(dir, created) {
		t.Fatal("setup: created.txt must not exist yet")
	}
	if err := os.WriteFile(created, nil, 0o644); err != nil { // absent → empty file
		t.Fatal(err)
	}
	if !pathExists(dir, created) {
		t.Error("a created empty file exists, and existence is what tells it from a no-op")
	}
	if err := os.Remove(empty); err != nil { // empty file → absent
		t.Fatal(err)
	}
	if pathExists(dir, empty) {
		t.Error("a deleted empty file is gone, and existence is what tells it from a no-op")
	}
	// Content alone cannot: all four read as "".
	for _, p := range []string{absent, empty, created} {
		if c, _ := readForChange(dir, p); c != "" {
			t.Errorf("%s reads as empty content, got %q", p, c)
		}
	}
	// And a path with real content is untouched by any of this.
	if c, _ := readForChange(dir, full); c != "x" {
		t.Errorf("a file with content still reads its content, got %q", c)
	}
}
