package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/testenv"
)

// Write's whole contract, minus the half this platform may not be able to answer: a reader sees
// the old file or the new one, and no temp litter survives. The mode half is next door.
//
// ⚠ **Split because a skip ends whatever test it stands in.** The mode assertions need
// testenv.NeedRestrictivePermissions, and standing here they took the content and litter checks
// down with them — or, before the guard existed, reported `an existing file keeps its permission
// bits, got -rw-rw-rw-` as a defect in Write. Windows has no POSIX mode: Chmod there toggles the
// read-only attribute and nothing else, so a file written 0640 reads back 0666. That is a fact
// about the filesystem, and content-is-whole is a promise Write makes everywhere.
func TestWriteReplacesWhole(t *testing.T) {
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
	if err := Write(filepath.Join(dir, "fresh.json"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "fresh.json")); string(b) != "x" {
		t.Fatalf("a new file is written with its contents, got %q", b)
	}

	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp litter survived: %s", e.Name())
		}
	}
}

// An existing file keeps its permission bits and a new one gets the ones it was given.
//
// Its own test because the guard on it skips, and because the promise itself is real where it can
// be made: config.toml is written 0600 and a rewrite that widened it to 0644 would hand every
// account on the machine a file holding provider keys. Where the filesystem cannot keep that
// promise, the skip is the record that it cannot — magi does not set an ACL in its place.
func TestWriteKeepsTheModeItWasGiven(t *testing.T) {
	testenv.NeedRestrictivePermissions(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The EXISTING file's mode wins: Write is publishing new bytes into a file somebody already
	// chose the permissions of, and quietly narrowing or widening them is not part of that.
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
