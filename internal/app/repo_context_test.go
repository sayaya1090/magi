package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoContextTwoLevelAndAnchorExcerpt(t *testing.T) {
	dir := t.TempDir()
	// A build anchor at top level whose opening lines must surface.
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("CC = gcc\nCFLAGS = -O2\nall:\n\t$(CC) $(CFLAGS) main.c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A source dir with a nested file (two-level tree must show the child).
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.c"), []byte("int main(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Noise dir that must be listed but NOT descended into.
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "leftpad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "leftpad", "index.js"), []byte("module.exports=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hidden entry must be skipped.
	if err := os.WriteFile(filepath.Join(dir, ".secret"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := repoContext(dir)

	if !strings.Contains(got, "Makefile") {
		t.Errorf("top-level Makefile missing:\n%s", got)
	}
	if !strings.Contains(got, "src/") || !strings.Contains(got, "main.c") {
		t.Errorf("two-level tree must show src/ and its child main.c:\n%s", got)
	}
	if !strings.Contains(got, "node_modules/") {
		t.Errorf("noise dir should still be listed:\n%s", got)
	}
	if strings.Contains(got, "leftpad") || strings.Contains(got, "index.js") {
		t.Errorf("noise dir must NOT be descended into:\n%s", got)
	}
	if strings.Contains(got, ".secret") {
		t.Errorf("hidden entry must be skipped:\n%s", got)
	}
	if !strings.Contains(got, "Makefile (excerpt)") || !strings.Contains(got, "CFLAGS = -O2") {
		t.Errorf("anchor excerpt must include Makefile opening lines:\n%s", got)
	}
}

// A source root's long tail is docs and build-variant files, and their names often sort ahead of
// every subdirectory. Under one shared alphabetical cap those files spend the whole budget and the
// tree lands with ZERO directories — the planner then has no structure to navigate and invents
// paths, which flow into the plan, the authored checks and the scout's seed. Directories must get
// their own cap and be listed first, so a late-sorting one still appears.
func TestRepoContextShowsDirectoriesFilesCannotCrowdOut(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// 20 files whose names all sort before any directory below.
	for i := 0; i < 20; i++ {
		p := filepath.Join(root, fmt.Sprintf("Aaa%02d.txt", i))
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 25 directories; the one that matters sorts last of all.
	for i := 0; i < 24; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("mid%02d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "zzz-target"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := repoContext(dir)

	if !strings.Contains(got, "zzz-target/") {
		t.Errorf("a late-sorting directory must survive the cut — files must not crowd out the tree:\n%s", got)
	}
	if !strings.Contains(got, "mid00/") || !strings.Contains(got, "mid23/") {
		t.Errorf("every directory within the directory cap must be listed:\n%s", got)
	}
	// Files stay tight: the file cap still bites, and its truncation is marked.
	if n := strings.Count(got, "Aaa"); n > 12 {
		t.Errorf("plain files must stay under their own cap, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "  …") {
		t.Errorf("a truncated class must be marked with an ellipsis:\n%s", got)
	}
	// Directories come before files so the file cap can never displace them.
	if di, fi := strings.Index(got, "mid00/"), strings.Index(got, "Aaa"); di < 0 || fi < 0 || di > fi {
		t.Errorf("directories must be listed before plain files (dir@%d file@%d):\n%s", di, fi, got)
	}
}

func TestRepoContextUnavailable(t *testing.T) {
	if got := repoContext(filepath.Join(t.TempDir(), "does-not-exist")); got != "(unavailable)" {
		t.Errorf("missing workdir must yield (unavailable), got %q", got)
	}
}

func TestHeadExcerptLineCap(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "many.txt")
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := headExcerpt(p, 5, 4096)
	if n := strings.Count(got, "\n"); n != 5 {
		t.Errorf("lineCap=5 must yield 5 lines, got %d:\n%q", n, got)
	}
	if headExcerpt(filepath.Join(dir, "nope"), 5, 4096) != "" {
		t.Error("unreadable file must yield empty excerpt")
	}
}
