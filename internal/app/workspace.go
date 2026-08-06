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
func (a *App) cloneWorkspace(ctx context.Context, parentDir string) (dir, base string, err error) {
	if !gitAvailable(parentDir) {
		return "", "", fmt.Errorf("an isolated workspace needs git and a repository here (%s is neither, or git is not installed) — "+
			"spawn without one to share the parent's directory", parentDir)
	}
	// A unique directory per child. Naming it after the session would need the session to exist
	// first, and a clone that fails would then leave an orphan session with nowhere to work; it
	// would also hand two concurrent children the same path.
	dir, err = os.MkdirTemp("", "magi-child-*")
	if err != nil {
		return "", "", fmt.Errorf("workspace: %w", err)
	}
	// --local hardlinks the object store instead of copying it, so a large repository costs
	// almost nothing per child. --no-checkout would save more but the child needs files to read.
	// git clone refuses a non-empty target, and MkdirTemp just made one; clone into a child path
	// and keep that as the workspace root.
	dir = filepath.Join(dir, "repo")
	if out, err := gitRun(ctx, parentDir, "clone", "--local", "--quiet", parentDir, dir); err != nil {
		os.RemoveAll(filepath.Dir(dir))
		return "", "", fmt.Errorf("workspace: git clone: %w: %s", err, out)
	}
	if err := carryUncommitted(ctx, parentDir, dir); err != nil {
		os.RemoveAll(filepath.Dir(dir))
		return "", "", err
	}
	// Pin the parent's state as a commit before the child touches anything.
	//
	// This is what makes the merge back exact. The clone now holds the parent's own uncommitted
	// work, and without a line drawn here a later "commit everything" would carry those changes
	// home as though the child had written them. With the baseline in place, `base..HEAD` is
	// precisely what the child did and nothing else.
	//
	// Committing here is safe in a way committing in the parent would not be: this is a throwaway
	// clone under the temp directory, not anybody's repository.
	if out, err := gitRun(ctx, dir, "checkout", "-q", "-b", childBranch); err != nil {
		os.RemoveAll(filepath.Dir(dir))
		return "", "", fmt.Errorf("workspace: branch: %w: %s", err, out)
	}
	base, err = commitAll(ctx, dir, "magi: the parent's tree as the child found it")
	if err != nil {
		os.RemoveAll(filepath.Dir(dir))
		return "", "", err
	}
	return dir, base, nil
}

// childBranch is the branch every child works on inside its own clone. One name is enough: each
// clone is its own repository, so there is nothing for a second child's branch to collide with.
const childBranch = "magi/child"

// commitAll commits everything in dir (tracked, modified and new) and returns the commit id. An
// empty tree commits nothing and returns the current HEAD, so a child that changed nothing is not
// an error.
func commitAll(ctx context.Context, dir, msg string) (string, error) {
	if out, err := gitRun(ctx, dir, "add", "-A"); err != nil {
		return "", fmt.Errorf("workspace: git add: %w: %s", err, out)
	}
	// --allow-empty keeps "the child did nothing" on the same path as every other outcome instead
	// of turning it into an error the caller has to special-case.
	if out, err := gitRun(ctx, dir, "-c", "user.email=magi@localhost", "-c", "user.name=magi",
		"commit", "-q", "--allow-empty", "-m", msg); err != nil {
		return "", fmt.Errorf("workspace: git commit: %w: %s", err, out)
	}
	head, err := gitRun(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("workspace: rev-parse: %w", err)
	}
	return strings.TrimSpace(head), nil
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

// MergeChildWork applies a child's commit range onto the parent's tree.
//
// The child's work is BaseCommit..HeadCommit in its own clone, so this is a real git operation on a
// real range rather than a copy of whatever files happen to be lying around. Three properties fall
// out of doing it that way:
//
//   - It is exactly the child's work. The parent's own uncommitted changes are below the baseline,
//     so they cannot come home a second time wearing the child's name.
//   - It applies onto a DIRTY parent. `git merge` refuses that, and the parent is dirty by
//     construction here — its uncommitted work is why the clone carried a patch in the first place.
//     --3way is git's own merge machinery, so a real conflict comes back as conflict markers rather
//     than as a silent overwrite.
//   - Nothing is committed in the parent. What lands is a working-tree change for a human to read,
//     which is the same line every other outward step in this package draws.
//
// It is never automatic. Whether a child's changes are wanted is the caller's judgement — a failed
// attempt's leavings are sometimes the evidence the next round needs.
func (a *App) MergeChildWork(ctx context.Context, parentDir, workspace, base, head string) error {
	if workspace == "" || base == "" || head == "" {
		return fmt.Errorf("merge: this child had no checkout of its own — there is no range to merge")
	}
	patch, err := gitRun(ctx, workspace, "diff", "--binary", base+".."+head)
	if err != nil {
		return fmt.Errorf("merge: reading %s..%s: %w: %s", base[:min(7, len(base))], head[:min(7, len(head))], err, patch)
	}
	if strings.TrimSpace(patch) == "" {
		return nil // the child changed nothing; not a failure, just nothing to do
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--3way", "--whitespace=nowarn", "-")
	cmd.Dir = parentDir
	cmd.Stdin = strings.NewReader(patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		// A conflict leaves markers in the files git could reach and says which ones. Report it
		// rather than rolling back: half-applied-with-markers is the state a human resolves, and
		// undoing it would throw away the child's work along with the conflict.
		return fmt.Errorf("merge: %w — git said: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
