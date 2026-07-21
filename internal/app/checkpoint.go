package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// workdirCheckpoint is a private git snapshot of a work-tree, used to roll a failed subagent
// attempt back to a clean starting point before a retry re-runs — the compile-compcert
// "self-clone retry loop", where each restart walked a dirty tree into the same wall.
//
// It lives in a scratch git-dir OUTSIDE the work-tree, so it never touches the user's own .git,
// stash, or HEAD. The checkpoint captures tracked AND untracked files at attempt start — which
// includes any pre-existing uncommitted user work — and restore returns exactly that state,
// discarding only what the attempt added afterwards (so the user's work is preserved by being part
// of the snapshot, the stash/pop insight). Best-effort throughout: a git or fs error degrades to
// "no checkpoint" rather than failing the spawn.
type workdirCheckpoint struct {
	gitDir  string
	workdir string
}

// newWorkdirCheckpoint snapshots workdir into a fresh scratch git-dir. Returns nil (not an error)
// when checkpointing is unavailable — no git, empty workdir, or any fs/git failure — so callers
// treat a nil checkpoint as "nothing to roll back to".
func newWorkdirCheckpoint(workdir string) *workdirCheckpoint {
	if strings.TrimSpace(workdir) == "" {
		return nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	gitDir, err := os.MkdirTemp("", "magi-cp-")
	if err != nil {
		return nil
	}
	c := &workdirCheckpoint{gitDir: gitDir, workdir: workdir}
	if err := c.run("init", "-q"); err != nil {
		c.cleanup()
		return nil
	}
	// add -A stages tracked+untracked (respecting any .gitignore in the tree); a directory named
	// .git in the work-tree is special and is never descended into, so the user's repo internals
	// stay out of the snapshot.
	if err := c.run("add", "-A"); err != nil {
		c.cleanup()
		return nil
	}
	if err := c.run("-c", "user.email=magi@local", "-c", "user.name=magi",
		"commit", "-q", "--allow-empty", "-m", "checkpoint"); err != nil {
		c.cleanup()
		return nil
	}
	return c
}

// restore returns the work-tree to the snapshot: tracked files reset, attempt-created untracked
// files removed. The user's real .git is doubly protected — git clean skips nested repositories and
// -e .git excludes it explicitly — so it is never deleted. Ignored files (build artifacts) are left
// in place (no -x); an attempt-0 checkpoint predates them anyway. Nil-safe.
func (c *workdirCheckpoint) restore() error {
	if c == nil {
		return nil
	}
	if err := c.run("reset", "-q", "--hard"); err != nil {
		return err
	}
	return c.run("clean", "-fdq", "-e", ".git")
}

// cleanup removes the scratch git-dir. Safe to call on nil or after a failed construction.
func (c *workdirCheckpoint) cleanup() {
	if c == nil || c.gitDir == "" {
		return
	}
	_ = os.RemoveAll(c.gitDir)
}

func (c *workdirCheckpoint) run(args ...string) error {
	full := append([]string{"--git-dir=" + c.gitDir, "--work-tree=" + c.workdir}, args...)
	cmd := exec.Command("git", full...)
	// Pin the dir/work-tree so an ambient GIT_DIR/GIT_WORK_TREE cannot redirect the operation onto
	// the user's real repository.
	cmd.Env = append(os.Environ(), "GIT_DIR="+c.gitDir, "GIT_WORK_TREE="+c.workdir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}
