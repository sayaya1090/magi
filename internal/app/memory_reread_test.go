package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type memPlat struct{ dir string }

func (m memPlat) ConfigDir() string { return m.dir }

func putMemo(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Editing AGENTS.md takes effect on the next turn, not the next start.
//
// The cache used to be permanent: read once, held for the life of the process. A person who wrote
// standing instructions into a running session got the old text on every later turn and nothing
// anywhere said why — and standing instructions that silently do not apply are worse than none,
// because the belief that they are in force outlives the fact.
func TestDurableMemoryIsRereadWhenTheFileChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	putMemo(t, path, "always one-line bullets")

	a := &App{}
	if got := a.projectMemory(dir); got != "always one-line bullets" {
		t.Fatalf("first read is wrong: %q", got)
	}

	// Move the clock forward so the change is visible in mtime on filesystems with coarse stamps.
	putMemo(t, path, "always two-line bullets")
	_ = os.Chtimes(path, time.Now().Add(time.Second), time.Now().Add(time.Second))

	if got := a.projectMemory(dir); got != "always two-line bullets" {
		t.Fatalf("edit did not take effect — the old text is still in force: %q", got)
	}
}

// A file that appears later is picked up too. Adding AGENTS.md to a workspace that had none is the
// most likely first edit anybody makes, and it is exactly the case a "read once" cache misses.
func TestDurableMemoryNoticesAFileThatAppears(t *testing.T) {
	dir := t.TempDir()
	a := &App{}
	if got := a.projectMemory(dir); got != "" {
		t.Fatalf("there is no file yet: %q", got)
	}
	putMemo(t, filepath.Join(dir, "AGENTS.md"), "use the blue accent colour")
	if got := a.projectMemory(dir); got != "use the blue accent colour" {
		t.Fatalf("a file that appeared was not picked up: %q", got)
	}
}

// …and one that is deleted stops applying. Otherwise instructions a person removed keep steering
// the model, which is the same defect pointed the other way.
func TestDurableMemoryNoticesAFileThatGoesAway(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	putMemo(t, path, "no bullets at all")
	a := &App{}
	if got := a.projectMemory(dir); got == "" {
		t.Fatal("the file is there")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := a.projectMemory(dir); got != "" {
		t.Fatalf("removed instructions are still in force: %q", got)
	}
}

// Nothing changed means nothing is re-read. The whole point of the stamp is that the common case
// costs three stats rather than three file reads.
func TestDurableMemoryIsNotRereadWhenNothingChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	putMemo(t, path, "hold this")

	a := &App{}
	_ = a.projectMemory(dir)

	// Rewrite the CONTENT without touching size or mtime: a real re-read would see the new bytes.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hold THIS"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := a.projectMemory(dir); got != "hold this" {
		t.Fatalf("the cache was bypassed — every turn now pays a read: %q", got)
	}
}

// The three sources are joined in order, and the global one comes first.
func TestDurableMemoryJoinsItsSourcesInOrder(t *testing.T) {
	cfg := t.TempDir()
	dir := t.TempDir()
	putMemo(t, filepath.Join(cfg, "AGENTS.md"), "global")
	putMemo(t, filepath.Join(dir, "AGENTS.md"), "project")
	putMemo(t, filepath.Join(dir, ".magi", "AGENTS.md"), "scoped")

	a := &App{plat: nil}
	if got := a.projectMemory(dir); got != "project\n\nscoped" {
		t.Fatalf("without a platform the two project files join: %q", got)
	}

	// And the stamp covers all three — a change in ANY of them re-reads.
	sources := memorySources(memPlat{dir: cfg}, dir)
	before := memoryStamp(sources)
	putMemo(t, filepath.Join(cfg, "AGENTS.md"), "global, edited")
	_ = os.Chtimes(filepath.Join(cfg, "AGENTS.md"), time.Now().Add(time.Second), time.Now().Add(time.Second))
	if memoryStamp(sources) == before {
		t.Fatal("a change to the global file did not move the stamp")
	}
}
