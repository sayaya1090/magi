package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A restore rolls tracked files back and removes attempt-created debris, while pre-existing
// (uncommitted) content survives because the checkpoint captured it.
func TestWorkdirCheckpointRestore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "keep.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp := newWorkdirCheckpoint(wd)
	if cp == nil {
		t.Fatal("checkpoint should be created")
	}
	defer cp.cleanup()

	// The attempt mutates tracked content and drops new files/dirs (build debris).
	_ = os.WriteFile(filepath.Join(wd, "keep.txt"), []byte("MUTATED"), 0o644)
	_ = os.WriteFile(filepath.Join(wd, "attempt.txt"), []byte("debris"), 0o644)
	_ = os.MkdirAll(filepath.Join(wd, "builddir"), 0o755)
	_ = os.WriteFile(filepath.Join(wd, "builddir", "artifact.o"), []byte("x"), 0o644)

	if err := cp.restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "keep.txt")); string(b) != "original" {
		t.Errorf("keep.txt = %q, want restored to original", string(b))
	}
	if _, err := os.Stat(filepath.Join(wd, "attempt.txt")); !os.IsNotExist(err) {
		t.Errorf("attempt debris file must be removed")
	}
	if _, err := os.Stat(filepath.Join(wd, "builddir")); !os.IsNotExist(err) {
		t.Errorf("attempt debris dir must be removed")
	}
}

// restore's clean never deletes the user's real .git directory.
func TestWorkdirCheckpointPreservesDotGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	wd := t.TempDir()
	_ = os.MkdirAll(filepath.Join(wd, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(wd, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wd, "f.txt"), []byte("a"), 0o644)

	cp := newWorkdirCheckpoint(wd)
	if cp == nil {
		t.Fatal("checkpoint should be created")
	}
	defer cp.cleanup()

	_ = os.WriteFile(filepath.Join(wd, "new.txt"), []byte("x"), 0o644)
	if err := cp.restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wd, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD must survive restore: %v", err)
	}
}

func TestWorkdirCheckpointFlagDefault(t *testing.T) {
	if workdirCheckpointEnabled() {
		t.Fatal("default must be OFF")
	}
	t.Setenv("MAGI_WORKDIR_CHECKPOINT", "1")
	if !workdirCheckpointEnabled() {
		t.Error("=1 must enable")
	}
}

// A nil checkpoint (unavailable) is a no-op, not a crash.
func TestWorkdirCheckpointNilSafe(t *testing.T) {
	var cp *workdirCheckpoint
	if err := cp.restore(); err != nil {
		t.Errorf("nil restore must be a no-op, got %v", err)
	}
	cp.cleanup() // must not panic
}
