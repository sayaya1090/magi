package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A child can work in its own checkout instead of the parent's directory.
//
// Two children writing the same tree is not a thing to undo afterwards — it is a thing to prevent,
// and the restore journal cannot help: a collision is two correct writes, not a mistake to roll
// back. Giving each child its own clone is what the multi-machine shape has for free (every host
// has its own working copy) and what single-machine tools build worktree machinery to imitate. This
// is the same model on one machine: allocate an area, clone into it, work there.
//
// OPT-IN, not the default. A read-only child — a planner, a reviewer — wants the parent's live tree
// including whatever is uncommitted, and a clone would cost it a copy to see a staler version of
// what it already had. That is the same call the tools that ship this make (their read-only agents
// turn isolation off); it is only inverted here so the safe direction is the one you get by saying
// nothing.
//
// The clone carries the parent's UNCOMMITTED work. `git clone` copies HEAD, and a child handed HEAD
// while the parent has an hour of unstaged edits is reading a version of the code that nobody is
// looking at. Tracked modifications go over as a patch and untracked files are copied, so what the
// child opens is what the parent sees.

// cloneWorkspace makes a child its own checkout of the parent's tree and returns its path.
//
// It FAILS rather than falling back to sharing. A caller asks for isolation because something would
// collide without it; handing back the shared directory with a note would produce exactly the
// collision it asked to avoid, and it would look like it worked.
func (a *App) cloneWorkspace(ctx context.Context, parentDir string) (string, error) {
	if !gitAvailable(parentDir) {
		return "", fmt.Errorf("an isolated workspace needs git and a repository here (%s is neither, or git is not installed) — "+
			"spawn without one to share the parent's directory", parentDir)
	}
	// A unique directory per child. Naming it after the session would need the session to exist
	// first, and a clone that fails would then leave an orphan session with nowhere to work; it
	// would also hand two concurrent children the same path.
	dir, err := os.MkdirTemp("", "magi-child-*")
	if err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	// --local hardlinks the object store instead of copying it, so a large repository costs
	// almost nothing per child. --no-checkout would save more but the child needs files to read.
	// git clone refuses a non-empty target, and MkdirTemp just made one; clone into a child path
	// and keep that as the workspace root.
	dir = filepath.Join(dir, "repo")
	if out, err := gitRun(ctx, parentDir, "clone", "--local", "--quiet", parentDir, dir); err != nil {
		os.RemoveAll(filepath.Dir(dir))
		return "", fmt.Errorf("workspace: git clone: %w: %s", err, out)
	}
	if err := carryUncommitted(ctx, parentDir, dir); err != nil {
		os.RemoveAll(filepath.Dir(dir))
		return "", err
	}
	return dir, nil
}

// carryUncommitted moves the parent's working state into the fresh clone: tracked modifications as
// a patch, untracked-and-not-ignored files as copies.
//
// Without this the child reads HEAD. On a tree with an hour of unstaged work that is a version of
// the code nobody is looking at, and every conclusion the child draws from it is about the wrong
// file — the failure mode is silent, because the child has no way to know.
func carryUncommitted(ctx context.Context, parentDir, dir string) error {
	patch, err := gitRun(ctx, parentDir, "diff", "HEAD", "--binary")
	if err != nil {
		return fmt.Errorf("workspace: reading the parent's uncommitted changes: %w", err)
	}
	if strings.TrimSpace(patch) != "" {
		cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-")
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(patch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("workspace: applying the parent's uncommitted changes: %w: %s", err, out)
		}
	}
	// Untracked files the parent has made but not added. Ignored ones are left out on purpose:
	// build output and dependency directories are the bulk of an ignored tree and the child can
	// rebuild what it needs.
	list, err := gitRun(ctx, parentDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return fmt.Errorf("workspace: listing the parent's untracked files: %w", err)
	}
	for _, rel := range strings.Split(strings.TrimSpace(list), "\n") {
		if rel = strings.TrimSpace(rel); rel == "" {
			continue
		}
		src, dst := filepath.Join(parentDir, rel), filepath.Join(dir, rel)
		b, err := os.ReadFile(src)
		if err != nil {
			continue // vanished between listing and reading, or unreadable; not worth failing over
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			continue
		}
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(src); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(dst, b, mode); err != nil {
			return fmt.Errorf("workspace: copying %s: %w", rel, err)
		}
	}
	return nil
}

// gitRun runs one git command in dir and returns its combined output.
func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
