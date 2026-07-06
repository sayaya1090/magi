package builtin

import (
	"os"
	"path/filepath"
	"testing"
)

// The workdir jail must also hold against a symlink that lives INSIDE the workdir but
// points OUTSIDE it — the lexical ".." check can't see that; only resolving symlinks can.
func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	work := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(work, "link")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	// Reading THROUGH an escaping symlink is denied.
	if _, err := resolvePath(work, "link/secret.txt"); err == nil {
		t.Error("must reject reading through a symlink that escapes the workdir")
	}
	// The escaping symlink itself is denied.
	if _, err := resolvePath(work, "link"); err == nil {
		t.Error("must reject a symlink that points outside the workdir")
	}

	// An in-jail symlink (points inside the workdir) is allowed.
	if err := os.Mkdir(filepath.Join(work, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(work, "sub", "ok.txt"), []byte("y"), 0o644)
	if err := os.Symlink(filepath.Join(work, "sub"), filepath.Join(work, "inlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePath(work, "inlink/ok.txt"); err != nil {
		t.Errorf("an in-jail symlink should be allowed: %v", err)
	}

	// Plain in-jail files still resolve, incl. a not-yet-created file (write path).
	os.WriteFile(filepath.Join(work, "normal.txt"), []byte("z"), 0o644)
	if _, err := resolvePath(work, "normal.txt"); err != nil {
		t.Errorf("a normal in-jail file should resolve: %v", err)
	}
	if _, err := resolvePath(work, "sub/new.txt"); err != nil {
		t.Errorf("a new file under a real in-jail dir should resolve: %v", err)
	}
}

// TestResolvePathRejectsBrokenSymlinkEscape guards O14: a symlink inside the
// workdir pointing to a not-yet-existing path OUTSIDE it. Lstat(link) succeeds so
// the jail found it as the deepest existing ancestor, but EvalSymlinks fails on the
// missing target — the old code failed open there, so a write to "link" followed
// the symlink and created a file outside the workdir. It must be rejected, while a
// broken symlink pointing back INSIDE the workdir stays allowed (write path).
func TestResolvePathRejectsBrokenSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(outside, 0o755)

	// link → OUTSIDE/escaped.txt, which does not exist yet.
	if err := os.Symlink(filepath.Join(outside, "escaped.txt"), filepath.Join(work, "link")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	if _, err := resolvePath(work, "link"); err == nil {
		t.Error("must reject a broken symlink that resolves outside the workdir")
	}

	// A broken symlink whose target is inside the workdir is a legitimate write path.
	if err := os.Symlink(filepath.Join(work, "future.txt"), filepath.Join(work, "inbroken")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePath(work, "inbroken"); err != nil {
		t.Errorf("a broken symlink pointing inside the workdir should resolve: %v", err)
	}

	// A symlink cycle must terminate (bounded recursion), not hang or panic.
	os.Symlink(filepath.Join(work, "b"), filepath.Join(work, "a"))
	os.Symlink(filepath.Join(work, "a"), filepath.Join(work, "b"))
	if _, err := resolvePath(work, "a"); err == nil {
		t.Error("a symlink cycle should be rejected, not accepted")
	}
}
