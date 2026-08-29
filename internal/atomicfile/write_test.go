package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Write's whole contract: a reader sees the old file or the new one, an existing file keeps its
// mode, a new one gets the given mode, a symlink's target is replaced without severing the link,
// and no temp litter survives.
func TestWriteReplacesWholeAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "new" {
		t.Fatalf("content: %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Fatalf("an existing file keeps its permission bits, got %v", fi.Mode().Perm())
	}

	q := filepath.Join(dir, "fresh.json")
	if err := Write(q, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(q); fi.Mode().Perm() != 0o600 {
		t.Fatalf("a new file gets perm, got %v", fi.Mode().Perm())
	}

	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp litter survived: %s", e.Name())
		}
	}
}

func TestWriteFollowsASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, "link.md")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := Write(link, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink must survive — severing it silently is what os.WriteFile would not do either")
	}
	if b, _ := os.ReadFile(target); string(b) != "after" {
		t.Fatalf("the pointed-to file is the one replaced, got %q", b)
	}
}
