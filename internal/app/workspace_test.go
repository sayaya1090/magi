package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

// skipWithoutGit: this whole file is about what git makes possible.
func skipWithoutGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func gitDo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A child's own checkout carries what the parent is ACTUALLY looking at, not HEAD.
//
// git clone copies the last commit. A child handed that while the parent has an hour of unstaged
// work reads a version of the code nobody is looking at, draws every conclusion from the wrong
// file, and has no way to notice — which is why this is the part worth pinning.
func TestAChildsCloneCarriesTheParentsUncommittedWork(t *testing.T) {
	skipWithoutGit(t)
	dir := gitRepo(t)
	write(t, dir, "committed.go", "package a // v1\n")
	gitDo(t, dir, "add", "-A")
	gitDo(t, dir, "commit", "-qm", "base")

	// Now the three states a clone has to reproduce.
	write(t, dir, "committed.go", "package a // v2 — edited, never committed\n")
	write(t, dir, "brand-new.go", "package a // untracked\n")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "ignored.log", "build noise\n")

	a, _, _ := spawnApp(t, &usageLLM{text: "done"})
	ws, _, err := a.cloneWorkspace(context.Background(), dir)
	if err != nil {
		t.Fatalf("cloneWorkspace: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(ws)) })

	if ws == dir {
		t.Fatal("the child was handed the parent's own directory — that is not isolation")
	}
	if got := read(t, ws, "committed.go"); !strings.Contains(got, "v2") {
		t.Errorf("the clone holds %q — it got HEAD, not what the parent is looking at", got)
	}
	if got := read(t, ws, "brand-new.go"); !strings.Contains(got, "untracked") {
		t.Error("an untracked file the parent made did not cross into the clone")
	}
	// Ignored files stay behind on purpose: build output is the bulk of an ignored tree.
	if _, err := os.Stat(filepath.Join(ws, "ignored.log")); !os.IsNotExist(err) {
		t.Error("an ignored file was copied into the clone")
	}
}

// Writing in the clone leaves the parent alone. This is the whole point: a collision between two
// children is two correct writes, and no journal can undo that — it has to be prevented.
func TestWritingInTheCloneDoesNotTouchTheParent(t *testing.T) {
	skipWithoutGit(t)
	dir := gitRepo(t)
	write(t, dir, "f.go", "package a // parent\n")
	gitDo(t, dir, "add", "-A")
	gitDo(t, dir, "commit", "-qm", "base")

	a, _, _ := spawnApp(t, &usageLLM{text: "done"})
	ws, _, err := a.cloneWorkspace(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(ws)) })

	write(t, ws, "f.go", "package a // the child's version\n")
	if got := read(t, dir, "f.go"); !strings.Contains(got, "parent") {
		t.Errorf("the child's write reached the parent's tree: %q", got)
	}

	// Two children get DIFFERENT areas — one shared path would be no isolation at all.
	ws2, _, err := a.cloneWorkspace(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(ws2)) })
	if ws2 == ws {
		t.Fatal("two children were given the same directory")
	}
}

// Asking for isolation where it cannot be given FAILS. Sharing quietly would hand back the very
// collision the caller asked to avoid, and it would look like it had worked.
func TestAskingForIsolationOutsideARepositoryFails(t *testing.T) {
	a, _, _ := spawnApp(t, &usageLLM{text: "done"})
	plain := t.TempDir() // no git repository here
	ws, _, err := a.cloneWorkspace(context.Background(), plain)
	if err == nil {
		t.Fatalf("a non-repository was silently given a workspace at %s", ws)
	}
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("the failure does not say what was missing: %v", err)
	}
	if !strings.Contains(err.Error(), "share") {
		t.Errorf("the failure does not say what to do instead: %v", err)
	}
}

// End to end: a spawn that asks for its own checkout gets one, works in it, and says where.
func TestASpawnWithItsOwnCheckoutWorksThereAndSaysWhere(t *testing.T) {
	skipWithoutGit(t)
	dir := gitRepo(t)
	write(t, dir, "target.go", "before\n")
	gitDo(t, dir, "add", "-A")
	gitDo(t, dir, "commit", "-qm", "base")

	a, parent, _ := spawnApp(t, &writingChildLLM{path: "target.go"})
	parent.Workdir = dir
	spawn, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "looper")

	res, err := spawn(context.Background(), port.SpawnSpec{
		Prompt: "change it", Tools: []string{"write", "read"}, Workspace: "clone"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if res.Workspace == "" {
		t.Fatal("the child asked for its own checkout and the result does not say where it was")
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(res.Workspace)) })

	// It wrote in its own area…
	if got := read(t, res.Workspace, "target.go"); !strings.Contains(got, "child") {
		t.Errorf("the child's workspace holds %q — it did not write there", got)
	}
	// …and the parent's tree is untouched.
	if got := read(t, dir, "target.go"); got != "before\n" {
		t.Errorf("the parent's tree changed to %q", got)
	}
}

// The merge is a COMMIT RANGE, so it brings back the child's work and nothing else.
//
// The clone carries the parent's own uncommitted edits, which is why the baseline commit matters:
// without that line drawn, "commit everything at the end" would carry those changes home wearing
// the child's name, and the parent would see its own work arrive as somebody else's.
func TestTheMergeBringsBackTheChildsWorkAndNotTheParentsOwn(t *testing.T) {
	skipWithoutGit(t)
	dir := gitRepo(t)
	write(t, dir, "shared.go", "committed\n")
	gitDo(t, dir, "add", "-A")
	gitDo(t, dir, "commit", "-qm", "base")
	// The parent's OWN uncommitted work, which the clone will carry but must not bring back.
	write(t, dir, "mine.go", "the parent is in the middle of this\n")

	a, _, _ := spawnApp(t, &usageLLM{text: "done"})
	ctx := context.Background()
	ws, base, err := a.cloneWorkspace(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(ws)) })

	// The child works in TWO rounds, so base..HEAD is a range and not just the last commit — a
	// merge that took only the tip would silently drop everything before it.
	write(t, ws, "first.go", "the child's first round\n")
	if _, err := commitAll(ctx, ws, "child round 1"); err != nil {
		t.Fatal(err)
	}
	write(t, ws, "childs.go", "the child wrote this\n")
	head, err := commitAll(ctx, ws, "child round 2")
	if err != nil {
		t.Fatal(err)
	}
	if head == base {
		t.Fatal("the child's commits did not land")
	}

	if err := a.MergeChildWork(ctx, dir, ws, base, head); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := read(t, dir, "childs.go"); !strings.Contains(got, "the child wrote this") {
		t.Errorf("the child's last round did not come back: %q", got)
	}
	if got := read(t, dir, "first.go"); !strings.Contains(got, "first round") {
		t.Errorf("the child's EARLIER round did not come back — the merge took the tip, not the range: %q", got)
	}
	// The parent's own file is untouched — not duplicated, not reverted, not conflicted.
	if got := read(t, dir, "mine.go"); got != "the parent is in the middle of this\n" {
		t.Errorf("the parent's own uncommitted work came back changed: %q", got)
	}
	// And nothing was committed in the parent: what landed is a working-tree change to read.
	out, _ := gitRun(ctx, dir, "log", "--oneline")
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Errorf("the merge committed in the parent's repository:\n%s", out)
	}
}

// A child that changed nothing merges cleanly and does nothing. Treating that as an error would
// make every caller special-case the ordinary outcome of a read-only round.
func TestMergingAChildThatChangedNothingIsNotAnError(t *testing.T) {
	skipWithoutGit(t)
	dir := gitRepo(t)
	write(t, dir, "f.go", "x\n")
	gitDo(t, dir, "add", "-A")
	gitDo(t, dir, "commit", "-qm", "base")

	a, _, _ := spawnApp(t, &usageLLM{text: "done"})
	ctx := context.Background()
	ws, base, err := a.cloneWorkspace(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(ws)) })
	head, err := commitAll(ctx, ws, "did nothing")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.MergeChildWork(ctx, dir, ws, base, head); err != nil {
		t.Errorf("merging an empty range failed: %v", err)
	}
}

// Merging a child that had no checkout of its own says so, rather than doing something arbitrary.
func TestMergingAChildThatSharedTheTreeIsRefused(t *testing.T) {
	a, _, _ := spawnApp(t, &usageLLM{text: "done"})
	err := a.MergeChildWork(context.Background(), t.TempDir(), "", "", "")
	if err == nil {
		t.Fatal("merging a child with no checkout was accepted")
	}
	if !strings.Contains(err.Error(), "no checkout") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}
