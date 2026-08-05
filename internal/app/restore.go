package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/session"
)

// Putting a failed child's work back.
//
// A loop that runs a child, judges it, and runs another needs the second round to start from a
// known state. It does not get one for free: a child shares the parent's working tree, and a child
// that half-finished leaves its edits behind for the next round to trip over.
//
// This is best-effort by construction and says so in what it returns. Three things put a file back
// and each covers what the ones before it cannot:
//
//  1. THE JOURNAL. The first state magi saw for every path the child touched, captured before the
//     touch — from write/edit's own before-snapshot and, for bash, from the destinations pulled out
//     of the command text. This is the only layer that can restore CONTENT, and it is the only one
//     that works with no git and no repository.
//  2. GIT, WHERE IT HELPS. Only for paths the child touched, only when the path is tracked, and
//     only `git checkout -- <path>`. Never `reset --hard`, never a stash, never a commit: the child
//     shares the parent's tree, so anything wider would throw away the USER's uncommitted work,
//     which magi was not asked to touch. git is an assist here, not the mechanism — its absence
//     costs precision on tracked files and nothing else.
//  3. THE SAGA. A mutation with no content to put back gets its inverse run instead: a file the
//     child created is removed. A move needs no separate handling — its source and its destination
//     are both journalled, so putting the source back and removing the destination IS the undo.
//     A delete whose content the journal holds is a write; one whose content it does not is
//     reported, not guessed at.
//
// And then the fourth thing, which matters more than the other three: WHAT COULD NOT BE PUT BACK IS
// NAMED. Half a restore reported as a clean one is worse than no restore at all — the next round
// would build on a tree it believes is clean. Every entry says what happened to it and why.
//
// What is out of scope and stays out: anything that is not a file. An installed package, a running
// server, a migrated database, a called API. A worktree would not have undone those either.

// RestoreOutcome is one path magi tried to put back.
type RestoreOutcome struct {
	Path     string
	Restored bool
	How      string // "journal", "git", "saga", or empty when nothing worked
	Reason   string // why not, when Restored is false — never left empty in that case
}

// journalEntry is the first state magi saw for one path this child touched.
type journalEntry struct {
	before   string
	readable bool // magi could read the content (not a directory, not past the compare cap)
	existed  bool
}

// restoreJournal is one child's first-seen states, keyed by path relative to the workdir.
type restoreJournal struct {
	mu      sync.Mutex
	workdir string
	first   map[string]journalEntry
	order   []string
}

// note records a path's state the FIRST time this child touches it. Later touches are ignored: the
// point of restoring is to get back to where the child found things, not to step back one edit.
func (j *restoreJournal) note(path, before string, readable, existed bool) {
	if j == nil || path == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.first == nil {
		j.first = map[string]journalEntry{}
	}
	if _, seen := j.first[path]; seen {
		return
	}
	j.first[path] = journalEntry{before: before, readable: readable, existed: existed}
	j.order = append(j.order, path)
}

// journalFor returns the child's journal, creating it on first use. Keyed by session so a parent
// running several children can put back exactly one of them.
func (a *App) journalFor(sid session.SessionID, workdir string) *restoreJournal {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.journals == nil {
		a.journals = map[session.SessionID]*restoreJournal{}
	}
	j, ok := a.journals[sid]
	if !ok {
		j = &restoreJournal{workdir: workdir}
		a.journals[sid] = j
	}
	return j
}

func (a *App) takeJournal(sid session.SessionID) *restoreJournal {
	a.mu.Lock()
	defer a.mu.Unlock()
	j := a.journals[sid]
	delete(a.journals, sid) // a child is restored once; keeping it would grow with every spawn
	return j
}

// RestoreChild puts back what a child changed, and reports every path either way.
//
// The journal is consumed: a second call finds nothing to do and says so, rather than replaying a
// restore against a tree the caller has since moved on from.
func (a *App) RestoreChild(ctx context.Context, sid session.SessionID) []RestoreOutcome {
	j := a.takeJournal(sid)
	if j == nil {
		return nil
	}
	j.mu.Lock()
	workdir, order, first := j.workdir, append([]string(nil), j.order...), map[string]journalEntry{}
	for k, v := range j.first {
		first[k] = v
	}
	j.mu.Unlock()

	var out []RestoreOutcome
	git := gitAvailable(workdir)
	sort.Strings(order) // stable output; content restores do not depend on each other
	for _, rel := range order {
		out = append(out, restoreOne(ctx, workdir, rel, first[rel], git))
	}
	return out
}

// restoreOne puts back a single path, trying the journal, then git, then the saga.
func restoreOne(ctx context.Context, workdir, rel string, e journalEntry, git bool) RestoreOutcome {
	abs := filepath.Join(workdir, rel)

	// The child CREATED it: putting things back means it is not there. This is the saga's inverse
	// of a create, and it needs no stored content.
	if !e.existed {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return RestoreOutcome{Path: rel, Reason: "the file the child created could not be removed: " + err.Error()}
		}
		return RestoreOutcome{Path: rel, Restored: true, How: "saga"}
	}

	// It existed and magi read it: write the bytes back. This is the only layer that does not need
	// git and does not need the path to be tracked.
	if e.readable {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return RestoreOutcome{Path: rel, Reason: "could not recreate the directory: " + err.Error()}
		}
		if err := os.WriteFile(abs, []byte(e.before), 0o644); err != nil {
			return RestoreOutcome{Path: rel, Reason: "could not write the original contents back: " + err.Error()}
		}
		return RestoreOutcome{Path: rel, Restored: true, How: "journal"}
	}

	// It existed and magi could NOT read it — a directory, or larger than it compares. git can
	// still put a tracked file back exactly; nothing else here can.
	if git {
		if err := gitCheckoutPath(ctx, workdir, rel); err == nil {
			return RestoreOutcome{Path: rel, Restored: true, How: "git"}
		}
	}
	reason := "magi never held this file's contents (a directory, or too large to snapshot)"
	if !git {
		reason += ", and there is no git checkout here to recover it from"
	} else {
		reason += ", and git does not track it"
	}
	return RestoreOutcome{Path: rel, Reason: reason}
}

// gitAvailable reports whether git is installed AND this workdir is inside a repository. Both have
// to hold, and neither is assumed: magi does not install anything to make it true.
func gitAvailable(workdir string) bool {
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = workdir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// gitCheckoutPath restores ONE path from the index. Scoped deliberately: the child shares the
// parent's tree, so a wider command would discard the user's own uncommitted work.
func gitCheckoutPath(ctx context.Context, workdir, rel string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "--", rel)
	cmd.Dir = workdir
	return cmd.Run()
}
