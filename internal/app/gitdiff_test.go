package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

func gitDiffApp(t *testing.T) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, &fakeLLM{}, builtin.Default(), bus.New(), platform.New(), Config{})
}

func gitRepo(t *testing.T, args ...[]string) string {
	t.Helper()
	dir := t.TempDir()
	base := [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@magi.test"},
		{"config", "user.name", "t"},
	}
	for _, a := range append(base, args...) {
		cmd := exec.Command("git", a...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	return dir
}

// A brand-new untracked file is the most common thing an agent creates. A plain
// `git diff` omits it; GitDiff must surface it WITH content so the council can see
// the work — otherwise the gate keeps voting "continue" for lack of evidence.
func TestGitDiffIncludesUntrackedFiles(t *testing.T) {
	dir := gitRepo(t, []string{"commit", "--allow-empty", "-q", "-m", "init"})
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("magi works\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff, err := gitDiffApp(t).GitDiff(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "hello.txt") {
		t.Fatalf("diff should name the new file:\n%s", diff)
	}
	if !strings.Contains(diff, "magi works") {
		t.Fatalf("diff should include the new file's content (the evidence the council needs):\n%s", diff)
	}
	if !strings.Contains(diff, "new file") {
		t.Fatalf("diff should mark it as a new file:\n%s", diff)
	}
}

// Untracked files must be surfaced even before the first commit (a fresh repo with
// no HEAD), via the empty-tree base.
func TestGitDiffUntrackedNoCommits(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := gitDiffApp(t).GitDiff(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "new.go") || !strings.Contains(diff, "package main") {
		t.Fatalf("diff should include the untracked file's content in a repo with no commits:\n%s", diff)
	}
}

// A modified tracked file is still reported.
func TestGitDiffTrackedModification(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", "a.txt"}, {"commit", "-q", "-m", "add a"}} {
		cmd := exec.Command("git", a...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := gitDiffApp(t).GitDiff(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-one") || !strings.Contains(diff, "+two") {
		t.Fatalf("diff should show the tracked modification:\n%s", diff)
	}
}

// No changes → empty diff (no spurious evidence for the council).
func TestGitDiffClean(t *testing.T) {
	dir := gitRepo(t, []string{"commit", "--allow-empty", "-q", "-m", "init"})
	diff, err := gitDiffApp(t).GitDiff(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(diff) != "" {
		t.Fatalf("clean tree should produce an empty diff, got:\n%s", diff)
	}
}

// The throwaway index must not disturb the real index: a file the user has staged
// stays staged after GitDiff runs.
func TestGitDiffPreservesRealIndex(t *testing.T) {
	dir := gitRepo(t, []string{"commit", "--allow-empty", "-q", "-m", "init"})
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "staged.txt")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if _, err := gitDiffApp(t).GitDiff(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	// staged.txt must still be staged (appear in `git diff --cached`).
	cached := exec.Command("git", "diff", "--cached", "--name-only")
	cached.Dir = dir
	out, err := cached.CombinedOutput()
	if err != nil {
		t.Fatalf("git diff --cached: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "staged.txt") {
		t.Fatalf("GitDiff disturbed the real index; staged.txt no longer staged. cached=%q", out)
	}
}
