package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every sentence built from a before/after pair states a fact about the WHOLE path — "byte-for-byte
// as it already was", "a content state it already held". readForChange stops at changeReadCap, so on
// a file past that the comparison covers a prefix and the sentence covers the file. A command could
// rewrite a byte past the cap and both sides would still match.
//
// Observed live (large-scale-text-editing, 2026-07-30): a 1,000,000-row CSV was copied and rewritten
// by vim, and the result said it was byte-for-byte unchanged — twice — off a 256 KiB look.
//
// The rule is the one the directory case already follows: a path magi cannot read the content of is
// one it cannot say anything true about, so it says nothing.
func TestAFileTooBigToCompareIsNotComparedAtAll(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(small, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := readForChange(dir, "small.txt"); !ok || got != "hello" {
		t.Fatalf("a readable file still compares: %q ok=%v", got, ok)
	}

	// Exactly at the cap is still fully read — the file IS its first changeReadCap bytes.
	atCap := filepath.Join(dir, "at.txt")
	if err := os.WriteFile(atCap, []byte(strings.Repeat("a", changeReadCap)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := readForChange(dir, "at.txt")
	if !ok || len(got) != changeReadCap {
		t.Errorf("a file that exactly fills the cap is complete: len=%d ok=%v", len(got), ok)
	}

	// One byte past it is not.
	over := filepath.Join(dir, "over.txt")
	if err := os.WriteFile(over, []byte(strings.Repeat("a", changeReadCap+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := readForChange(dir, "over.txt"); ok || got != "" {
		t.Errorf("a file past the cap yields no comparable content: len=%d ok=%v", len(got), ok)
	}
}

// …and the guard says nothing about such a path, rather than something false. Both sides come back
// empty, which is the shape noteEdit already refuses to read as a rewrite.
func TestNoNoOpClaimAboutAFilePastTheReadCap(t *testing.T) {
	g := newRunGuard(nil)
	// This is what the call sites hand over once readForChange declines: "" on both sides.
	if warn, reg := g.noteEdit("huge.csv", "", ""); warn != "" || reg {
		t.Errorf("nothing may be claimed about a path with no comparable content: warn=%q regressed=%v", warn, reg)
	}
}
