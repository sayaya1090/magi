package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordChange keeps the FIRST before and the LATEST after per file (so multiple edits
// collapse to one net change), and buildCouncilChanges renders each as a "### path" header
// plus the before→after line diff.
func TestCouncilChangeReconstruction(t *testing.T) {
	g := newRunGuard()
	g.recordChange("a.go", "old line\n", "new line\n")
	g.recordChange("a.go", "IGNORED-2ND-BEFORE", "new line 2\n") // before stays first; after updates
	g.recordChange("b.txt", "", "created\n")                     // new file

	cs := g.changeSet()
	if len(cs) != 2 {
		t.Fatalf("want 2 changed files, got %d", len(cs))
	}
	if cs[0].path != "a.go" || cs[0].before != "old line\n" || cs[0].after != "new line 2\n" {
		t.Errorf("a.go should collapse to first-before/last-after, got %+v", cs[0])
	}
	out := buildCouncilChanges(cs)
	for _, want := range []string{"### a.go", "-old line", "+new line 2", "### b.txt", "+created"} {
		if !strings.Contains(out, want) {
			t.Errorf("change evidence missing %q:\n%s", want, out)
		}
	}
}

// readForChange reads a capped prefix relative to workdir (empty on a missing file);
// relForChange maps a tool path back to a workdir-relative display path.
func TestReadAndRelForChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readForChange(dir, "x.go"); got != "hello" {
		t.Errorf("readForChange(relative) = %q, want hello", got)
	}
	if got := readForChange(dir, filepath.Join(dir, "x.go")); got != "hello" {
		t.Errorf("readForChange(absolute) = %q, want hello", got)
	}
	if got := readForChange(dir, "missing.go"); got != "" {
		t.Errorf("readForChange(missing) = %q, want empty", got)
	}
	if got := relForChange(dir, filepath.Join(dir, "x.go")); got != "x.go" {
		t.Errorf("relForChange(absolute) = %q, want x.go", got)
	}
	if got := relForChange(dir, "sub/y.go"); got != "sub/y.go" {
		t.Errorf("relForChange(relative) = %q, want sub/y.go", got)
	}
}
