package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A workspace with a repository behind it needs none of this: the delete undoes from the object
// store, and a directory INSIDE a checkout is covered by that checkout.
func TestRecoverableTreeFindsTheRepositoryAbove(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if recoverableTree(deep) {
		t.Fatal("a tree with no .git anywhere above it reported history")
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !recoverableTree(deep) {
		t.Error("a directory inside a checkout is covered by that checkout")
	}
	// A worktree's .git is a FILE, and means the same thing.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, ".git"), []byte("gitdir: /elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !recoverableTree(other) {
		t.Error("a worktree keeps its .git as a file and still has history")
	}
}

// An edit's way back is a hard link: the old contents survive because the write replaces the file
// atomically, and holding a second name for that inode costs no disk at all.
func TestAnEditKeepsWhatItReplaces(t *testing.T) {
	wd := t.TempDir()
	f := filepath.Join(wd, "note.md")
	if err := os.WriteFile(f, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	turn := time.Now()
	where, kept, err := keepBeforeEditing(wd, "note.md", turn, nil)
	if err != nil || !kept {
		t.Fatalf("the previous contents were not kept: %v %v", kept, err)
	}
	// The write that follows: atomic, so the old inode lives on under the kept name.
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, f); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != "after" {
		t.Fatalf("the edit did not land: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, where)); string(b) != "before" {
		t.Errorf("what the edit replaced is gone: %q", b)
	}
	// Once per file per turn: a second edit in the same turn keeps the FIRST state.
	if _, again, _ := keepBeforeEditing(wd, "note.md", turn, nil); again {
		t.Error("a second edit in one turn overwrote the state the turn started from")
	}
}

// The sweep is always one turn behind itself, so the batch a person is most likely to want back —
// the one the turn that just ended made — is still there.
func TestTheSweepKeepsTheNewestBatches(t *testing.T) {
	wd := t.TempDir()
	trash := filepath.Join(wd, trashDirName)
	names := []string{"20260101-000000.000", "20260102-000000.000", "20260103-000000.000", "20260104-000000.000"}
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(trash, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, n := range names[:2] {
		if err := os.Chtimes(filepath.Join(trash, n), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if got := sweepTrash(wd); got != 2 {
		t.Fatalf("the sweep took %d batches, want the two oldest", got)
	}
	// Age is what sends a batch away; the two newest are kept whatever their age, and a batch
	// still inside the retention window stays even when it is neither.
	fresh := filepath.Join(trash, "20260105-000000.000")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := sweepTrash(wd); got != 0 {
		t.Fatalf("the sweep took %d recent batches; only age sends one away", got)
	}
	for _, n := range names[2:] {
		if _, err := os.Stat(filepath.Join(trash, n)); err != nil {
			t.Errorf("%s was swept; the two newest are the way back that must survive", n)
		}
	}
}

// A file this turn made has no "before the turn" to keep, and neither the delete nor the edit
// path pretends otherwise: what the run created is the run's own output.
func TestWhatThisTurnMadeIsNotHeldOnTo(t *testing.T) {
	wd := t.TempDir()
	f := filepath.Join(wd, "fresh.txt")
	if err := os.WriteFile(f, []byte("made here"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := func(p string) bool { return strings.HasSuffix(p, "fresh.txt") }
	if _, kept, _ := keepBeforeEditing(wd, "fresh.txt", time.Now(), mine); kept {
		t.Error("the first draft of a file this turn created was kept as if it predated the turn")
	}
	// And without that knowledge it IS kept, so the exemption is what decided it.
	if _, kept, _ := keepBeforeEditing(wd, "fresh.txt", time.Now(), nil); !kept {
		t.Error("a file nobody claims was left unheld")
	}
}

// With no history behind the tree, an in-tree recursive delete is ASKED about rather than acted
// on. A regex over command text cannot know what a shell will delete — `echo "rm -rf build" >
// clean.sh` matches it — which a question survives and anything touching files does not.
func TestATreeWithNoHistoryIsAskedAboutItsDeletes(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	why, yes := needsCouncilBeforeRunning(wd, "rm -rf data", nil)
	if !yes {
		t.Fatal("a delete with nothing to restore it from was let through")
	}
	if !strings.Contains(why, "no git history") {
		t.Errorf("the reason must say what is missing: %q", why)
	}
	// Nothing was moved: the question is the whole of it.
	if _, err := os.Stat(filepath.Join(wd, "data")); err != nil {
		t.Error("asking about a delete must not itself touch the tree")
	}
	// A checkout keeps the old behaviour — the delete undoes from the object store.
	if err := os.Mkdir(filepath.Join(wd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, yes := needsCouncilBeforeRunning(wd, "rm -rf data", nil); yes {
		t.Error("a checkout was gated on a delete it can undo by itself")
	}
}

// The run's own output and the scratch area stay exempt even with no history: the gate has always
// read them as the run's own, and gating them would fire on every build directory.
func TestTheRunsOwnOutputIsStillNotGated(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := func(p string) bool { return strings.HasSuffix(p, "out") }
	if _, yes := needsCouncilBeforeRunning(wd, "rm -rf out", mine); yes {
		t.Error("the run was asked about deleting what it had just made")
	}
}

// "Cannot tell" is not "no history": a workspace this process cannot even see must not have a
// question put in front of every command it runs.
func TestAnUnseeableWorkspaceAddsNoFriction(t *testing.T) {
	if !recoverableTree("/no/such/place/anywhere") {
		t.Error("a workspace that cannot be stat'd was treated as one with nothing to restore from")
	}
	if why, yes := needsCouncilBeforeRunning("/no/such/place/anywhere", "rm -rf data", nil); yes {
		t.Errorf("a command in an unseeable workspace was gated: %q", why)
	}
}

// The question is asked about what is actually there — and about a glob, which is the one target
// this cannot resolve and the one with the most to lose in a tree with no history.
func TestTheQuestionSkipsWhatIsNotThereAndAsksAboutGlobs(t *testing.T) {
	wd := t.TempDir()
	if _, yes := needsCouncilBeforeRunning(wd, "rm -rf build", nil); yes {
		t.Error("a cleanup of a directory that does not exist was gated")
	}
	if _, yes := needsCouncilBeforeRunning(wd, "rm -rf *", nil); !yes {
		t.Error("`rm -rf *` in a tree with no history is the one to ask about, and it was let through")
	}
	if err := os.MkdirAll(filepath.Join(wd, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, yes := needsCouncilBeforeRunning(wd, "rm -rf build", nil); !yes {
		t.Error("a directory that IS there, with nothing to restore it from, must be asked about")
	}
}
