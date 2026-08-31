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
// The irreversible gate's premise is written down in irreversible.go: inside a git workspace almost
// nothing is irreversible, so a recursive delete of a path the tree contains is ordinary and
// gating it would fire constantly and get the gate turned off. That premise is entirely git's. A
// workspace with no repository behind it has no object store to restore from, and the same
// `rm -rf` there is exactly what the gate exists to stop — while looking, to the code, like the
// harmless case.
//
// Rather than ask about every build directory in such a tree, the tree is given the thing it was
// missing: the target is MOVED to a trash beside it before the command runs. A move, not a copy,
// because a rename within one filesystem is one directory entry and costs nothing whether the
// target is a file or forty thousand of them — which is why the trash lives INSIDE the workspace,
// the one place a rename is guaranteed not to cross a device.
//
// It is announced, never silent. This repository's rule is that magi does not do things the model
// cannot see: the tool result says what moved and where, and the `rm` that follows finds nothing
// and succeeds, which is what `-f` means.

// trashDirName is where a workspace keeps what was taken out of it.
const trashDirName = ".magi/trash"

// trashRetention bounds what the sweep keeps when it cannot tell turns apart.
const trashRetention = 7 * 24 * time.Hour

// recoverableTree reports whether this workspace has history to restore a delete from — a .git
// anywhere at or above it, since a directory inside a checkout is covered by that checkout.
//
// The entry is stat'd rather than opened: a worktree's .git is a FILE, and both forms mean the
// same thing here.
func recoverableTree(workdir string) bool {
	dir := filepath.Clean(workdir)
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
	if strings.TrimSpace(path) == "" {
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
	real := abs
	if _, serr := os.Lstat(real); serr != nil {
		return "", false, nil // it does not exist yet — a new file has no previous contents
	}
	fi, serr := os.Lstat(real)
	if serr != nil || !fi.Mode().IsRegular() {
		return "", false, nil
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

// sweepTrash clears what a workspace no longer needs to keep, and answers how many entries it
// took. Called when a turn lands.
//
// The newest batch is KEPT, whatever its age. The moment a person is most likely to want something
// back is just after the turn that removed it — which is exactly when a sweep that ran at the end
// of the turn would have taken it. So the sweep is always one turn behind itself: this turn's
// rescues survive into the next one, and older ones go. Anything past the retention goes
// regardless, so a workspace nobody returns to does not keep growing.
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
	swept := 0
	for _, n := range names {
		if keep[n] {
			continue // the last two turns' way back, kept whatever their age
		}
		p := filepath.Join(trash, n)
		if fi, serr := os.Stat(p); serr == nil && fi.ModTime().After(cut) {
			continue // still inside the retention window
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
