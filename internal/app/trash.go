package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A way back for a workspace that has no history.
//
// The irreversible gate's premise is written down in irreversible.go: inside a git workspace
// almost nothing is irreversible, so a recursive delete of a path the tree contains is ordinary
// and gating it would fire constantly. That premise is entirely git's, and a workspace with no
// repository behind it has no object store to restore from.
//
// The two halves of the answer are deliberately different, and the difference is what a target
// IS. A delete's target is a token in a shell command, and no regex can know what the shell will
// do with it — an earlier version of this file moved files on that guess and moved the wrong ones
// (a quoted path split on its space, a symlink named a tree outside the workspace, a command that
// only MENTIONED rm matched). So a delete is asked about instead: the gate lifts its in-tree
// exemption, and a wrong guess costs a turn rather than data.
//
// An edit's target is not a guess. It is the writing tool's own declared `path`, so this file can
// act on it: the previous contents are held by a HARD LINK before the write, which costs one
// directory entry and no disk at all because magi's writers replace a file atomically and the old
// contents keep living in their own inode. Once per file per turn, never for what the run itself
// created, and always said in the tool result — a rescue the model cannot see is one it cannot
// undo.

// trashDirName is where a workspace keeps the contents its edits replaced.
const trashDirName = ".magi/trash"

// trashRetention and trashBatches bound what a workspace keeps: how old, and how many.
//
// The count is the one that binds. A batch is made per turn that edits something, and each holds
// hard links to inodes nothing else refers to any more — so age alone let a two-hundred-turn run
// keep two hundred dead copies of the same file, which is not "costs no disk" but a version
// history nobody asked for on a disk the bench already runs out of.
const (
	trashRetention = 7 * 24 * time.Hour
	trashBatches   = 8
)

// recoverableTree reports whether this workspace has history to restore a delete from — a .git
// anywhere at or above it, since a directory inside a checkout is covered by that checkout.
//
// The entry is stat'd rather than opened: a worktree's .git is a FILE, and both forms mean the
// same thing here.
func recoverableTree(workdir string) bool {
	if strings.TrimSpace(workdir) == "" {
		return true // no workspace named; this mechanism has nothing to say about it
	}
	// Resolved, for the reason keepBeforeEditing resolves: a workspace reached through a symlink
	// walks its own path upward and never meets the repository it actually lives in, so a real
	// checkout reads as historyless — gating its deletes and writing rescue links into a tracked
	// tree.
	dir := realDir(workdir)
	// A workspace this process cannot see is not a workspace this process can say anything about.
	// The answer here decides whether to ADD friction, so "cannot tell" has to mean "do not add
	// it": a path that is not there has nothing to delete, and a directory that cannot be stat'd
	// would otherwise put a council question in front of every command in a run that was going to
	// fail on its own.
	if _, err := os.Stat(dir); err != nil {
		return true
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// realDir is the workspace with its symlinks resolved, so a path derived from it can be compared
// with a path the filesystem resolved. Unresolvable falls back to the cleaned form: the comparison
// is then no worse than it was.
func realDir(workdir string) string {
	c := filepath.Clean(workdir)
	if r, err := filepath.EvalSymlinks(c); err == nil {
		return r
	}
	return c
}

// keepBeforeEditing holds on to a file's current contents before a tool replaces them, in a
// workspace with no history to get them back from, and answers where they are kept.
//
// A HARD LINK, which is the edit's version of the move above: magi's writing tools replace a file
// atomically (a temp file and a rename — see builtin/atomicwrite.go), so the old contents keep
// living in their own inode after the write, and a second name for that inode costs one directory
// entry and not one byte of disk. Copying would cost the size of the file for the same answer.
//
// Once per file per turn. Ten edits to one file in a turn are one thing a person wants back — the
// state it was in before the turn started — and keying the batch on the turn makes "the first
// touch wins" fall out of the link already existing.
func keepBeforeEditing(workdir, path string, turn time.Time, mine func(string) bool) (where string, kept bool, err error) {
	// An empty workspace is not the process's own directory. realDir("") answers ".", which is
	// wherever the daemon was started, and a session created without a workdir would plant its
	// rescues there.
	if strings.TrimSpace(path) == "" || strings.TrimSpace(workdir) == "" {
		return "", false, nil
	}
	abs := absTarget(workdir, path)
	// The workspace RESOLVED, because the file is resolved below and the two are compared: on
	// macOS a temp workspace is reached through /var while the file resolves to /private/var, and
	// an unresolved root makes every file in the tree look like it is outside it.
	root := realDir(workdir)
	if r, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = r
	}
	trash := filepath.Join(root, trashDirName)
	if abs == root || strings.HasPrefix(abs, trash+string(filepath.Separator)) {
		return "", false, nil
	}
	if rel, rerr := filepath.Rel(root, abs); rerr != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false, nil // outside the tree is not this mechanism's business
	}
	// The RESOLVED file, because an atomic write follows a symlink and replaces what it points
	// at: linking the link would hold on to the wrong thing.
	// abs was resolved above, so this is the real file: one stat answers both "is it there" and
	// "is it something a link can hold".
	real := abs
	fi, serr := os.Lstat(real)
	if serr != nil || !fi.Mode().IsRegular() {
		return "", false, nil // absent, or not a plain file — a new file has no previous contents
	}
	rel, rerr := filepath.Rel(root, real)
	if rerr != nil || strings.HasPrefix(rel, "..") {
		return "", false, nil
	}
	// What this turn made needs no way back to before the turn: there was no before. The delete
	// path has always read it this way (the gate's `mine`), and an edit to a file the turn itself
	// created is the same fact — holding its first draft would keep a state nobody had.
	if mine != nil && (mine(real) || mine(abs)) {
		return "", false, nil
	}
	dst := filepath.Join(trash, turn.UTC().Format("20060102-150405.000"), "edits", rel)
	if _, err := os.Lstat(dst); err == nil {
		return "", false, nil // this turn already holds this file's earlier state
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", false, err
	}
	if err := os.Link(real, dst); err != nil {
		// Hard links fail on some filesystems and across devices. Falling back to a copy would
		// spend the size of the file to keep a promise nobody made; the caller says nothing and
		// the edit proceeds as it always did.
		return "", false, err
	}
	kept = true
	if r, e := filepath.Rel(root, dst); e == nil {
		return r, kept, nil
	}
	return dst, kept, nil
}

// sweepTrash clears what a workspace no longer needs to keep, and answers how many batches it
// took. Called at the START of a turn.
//
// The two newest batches are KEPT, whatever their age. The moment a person is most likely to want
// something back is just after the turn that replaced it — which is exactly when a sweep at the
// end of that turn would have taken it — so this runs at the start of the next one instead, which
// also means it runs for the turns that end by error rather than by landing.
//
// Two bounds past that, and the COUNT is the one that binds: a batch per editing turn, each
// holding links to inodes nothing else refers to any more, is a version history that age alone
// would let grow for a week.
func sweepTrash(workdir string) int {
	trash := filepath.Join(filepath.Clean(workdir), trashDirName)
	entries, err := os.ReadDir(trash)
	if err != nil {
		return 0
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return 0
	}
	sort.Strings(names) // the stamp is the name, so this is oldest-first
	keep := map[string]bool{names[len(names)-1]: true}
	if len(names) > 1 {
		keep[names[len(names)-2]] = true
	}
	cut := time.Now().Add(-trashRetention)
	// Everything past the newest trashBatches goes whatever its age; within that, age decides.
	overCount := len(names) - trashBatches
	swept := 0
	for i, n := range names {
		if keep[n] {
			continue // the last two turns' way back, kept whatever their age
		}
		p := filepath.Join(trash, n)
		if i >= overCount {
			if fi, serr := os.Stat(p); serr == nil && fi.ModTime().After(cut) {
				continue // inside both bounds
			}
		}
		if os.RemoveAll(p) == nil {
			swept++
		}
	}
	if swept > 0 {
		fmt.Fprintf(os.Stderr, "magi: cleared %d old rescue(s) from %s\n", swept, trashDirName)
	}
	return swept
}
