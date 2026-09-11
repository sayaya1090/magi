package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/testenv"
)

// atomicWriteFile replaces content and leaves no temp file behind.
func TestAtomicWriteFileReplacesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("new content"), 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Errorf("content = %q, want %q", got, "new content")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// An existing file keeps its permission bits across the rename.
func TestAtomicWriteFilePreservesMode(t *testing.T) {
	testenv.NeedExecutableBit(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o, want 755", info.Mode().Perm())
	}
}

// A new file is created with the given perm.
func TestAtomicWriteFileCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.txt")
	if err := atomicWriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	// The file is there with what was written — asked before the mode, because the mode question
	// is one this platform may not be able to answer and the creation question always is.
	if b, err := os.ReadFile(path); err != nil || string(b) != "hello" {
		t.Fatalf("the file was not created with its contents: %q (%v)", b, err)
	}
	// Windows has no POSIX mode: Chmod there toggles the read-only attribute and nothing else, so a
	// file written 0600 reads back 0666. "This filesystem has no mode to keep" and "atomicWriteFile
	// dropped the mode" are different facts and only the second is a defect. Measured 2026-09-11
	// here as `mode = 666, want 600`.
	testenv.NeedRestrictivePermissions(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

// A missing parent directory fails cleanly without touching anything.
func TestAtomicWriteFileMissingDir(t *testing.T) {
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "no", "such", "f.txt"), []byte("x"), 0o644); err == nil {
		t.Error("expected error for missing parent directory")
	}
}

// Writing through a symlink replaces the target file and leaves the link intact,
// matching os.WriteFile's follow semantics (no silent link severing).
func TestAtomicWriteFileFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := atomicWriteFile(link, []byte("new"), 0o644); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	// The link must still be a link, and the target must hold the new content.
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced with a regular file")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("target content = %q, want %q", got, "new")
	}
}
