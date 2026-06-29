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
