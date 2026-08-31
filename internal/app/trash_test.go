package app

import (
	"fmt"
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
	// All four are FRESH, so age cannot be what decides: only the count bound can send any of
	// them away, and only the keep-two rule can save the last two. Aged fixtures made this test
	// pass with the rule deleted outright.
	for i := 0; i < trashBatches; i++ {
		if err := os.MkdirAll(filepath.Join(trash, fmt.Sprintf("20260201-0000%02d.000", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	total := 4 + trashBatches
	if got := sweepTrash(wd); got != total-trashBatches {
		t.Fatalf("the sweep took %d of %d fresh batches, want the count bound to take %d",
			got, total, total-trashBatches)
	}
	// The two newest survive a sweep even when everything is old enough to go.
	rest, _ := os.ReadDir(trash)
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, e := range rest {
		p := filepath.Join(trash, e.Name())
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	sweepTrash(wd)
	left, _ := os.ReadDir(trash)
	if len(left) != 2 {
		t.Fatalf("%d batches survived an all-old sweep; the two newest are the rule", len(left))
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

// The exemption a build can satisfy: what the workspace held when magi arrived is the only thing
// in it nobody here can make again. A directory this session produced is not that, however many
// turns ago it appeared — which the turn-scoped record could not say, because the turn that ran
// `make` is over by the time `rm -rf build` arrives.
func TestWhatThisSessionBuiltIsNotAskedAbout(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "main.c"), []byte("int main(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	arrival := indexWorkspace(wd)
	// The build lands after arrival, in some earlier turn — no runGuard remembers it now.
	if err := os.MkdirAll(filepath.Join(wd, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "build", "app"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, yes := needsCouncilBeforeRunningSince(wd, "rm -rf build", nil, arrival); yes {
		t.Error("cleaning what this session built was gated — every rebuild would pay for it")
	}
	// What the person brought is still asked about.
	if _, yes := needsCouncilBeforeRunningSince(wd, "rm -rf main.c", nil, arrival); !yes {
		t.Error("a file that was here on arrival was let through")
	}
	// And a directory that HOLDS one of theirs is theirs, whatever else it has gained since.
	if err := os.MkdirAll(filepath.Join(wd, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "src", "theirs.c"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	arrival2 := indexWorkspace(wd)
	if err := os.WriteFile(filepath.Join(wd, "src", "generated.c"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, yes := needsCouncilBeforeRunningSince(wd, "rm -rf src", nil, arrival2); !yes {
		t.Error("a tree holding what the person brought was treated as this session's own")
	}
}

// What the shell can rewrite is larger than what globs: a brace expansion and a quoted path both
// name something Lstat cannot find, and reading that as "not there" fails OPEN.
func TestWhatTheShellRewritesIsTreatedAsPresent(t *testing.T) {
	wd := t.TempDir()
	for _, d := range []string{"build", "dist", "my dir"} {
		if err := os.MkdirAll(filepath.Join(wd, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range []string{"rm -rf {build,dist}", `rm -rf "my dir"`, "rm -rf *", `rm -rf \build`} {
		if _, yes := needsCouncilBeforeRunning(wd, cmd, nil); !yes {
			t.Errorf("%q deletes and was not asked about", cmd)
		}
	}
}

// A checkout reached through a symlink is still a checkout: walking the symlinked path upward
// never meets the repository, and calling it historyless would gate its deletes and write rescue
// links into a tracked tree.
func TestASymlinkedWorkspaceFindsItsRepository(t *testing.T) {
	real := t.TempDir()
	if err := os.Mkdir(filepath.Join(real, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(real, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(sub, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if !recoverableTree(link) {
		t.Error("a checkout reached through a symlink was read as having no history")
	}
}

// An empty workspace is not the process's own directory.
func TestAnEmptyWorkspaceIsNotTheDaemonsDirectory(t *testing.T) {
	if _, kept, _ := keepBeforeEditing("", "note.md", time.Now(), nil); kept {
		t.Error("a session with no workdir planted its rescues wherever the daemon was started")
	}
}
