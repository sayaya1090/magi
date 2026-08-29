package app

import (
	"context"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// preexistingDirtPaths lists the worktree paths that were already modified when the turn began.
// A GitFacts error (not a checkout, no git) is an empty list — nothing to disclaim.
func preexistingDirtPaths(st GitState, err error) []string {
	if err != nil {
		return nil
	}
	// Modified paths first, untracked after: the banner's list is capped, and a tree full of
	// untracked scratch would otherwise push the changes that actually matter past the cut. Git
	// happens to emit them in this order already; ordering them here makes it a property of this
	// function rather than of git's output.
	out := make([]string, 0, len(st.Changes))
	for _, c := range st.Changes {
		if c.Kind != "untracked" {
			out = append(out, c.Path)
		}
	}
	for _, c := range st.Changes {
		if c.Kind == "untracked" {
			out = append(out, c.Path)
		}
	}
	return out
}

// gitProbeBound matches the console's own bound on the same command.
const gitProbeBound = 10 * time.Second

// dirtyBeforeTurn is the turn-start probe. Bounded, because this runs on the hottest path there
// is, before anything is drawn: a status over a tree with a hundred thousand untracked files (or
// one waiting on index.lock, or a network mount) is a walk, not an answer, and an unbounded one
// wedges the turn indistinguishably from a stuck model. The console pane bounds its own call for
// exactly this reason.
func (a *App) dirtyBeforeTurn(ctx context.Context, workdir string) []string {
	pctx, cancel := context.WithTimeout(ctx, gitProbeBound)
	defer cancel()
	return preexistingDirtPaths(a.GitFacts(pctx, workdir))
}

// preexistingDirtBanner renders the disclaimer the council reads beside the turn's evidence.
//
// The members judge what they are shown, and what they were shown once included a `git status`
// echo carrying a modification from BEFORE the turn: three members read it as the turn's doing
// and ordered it reverted (live-QA Q2 — an uncommitted README destroyed on a 3:0 continue). The
// baseline is a fact only the turn's start can record, so it is recorded there and said here.
func preexistingDirtBanner(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	const cap = 20
	shown := paths
	more := ""
	if len(shown) > cap {
		shown = shown[:cap]
		more = " …"
	}
	return "── ALREADY MODIFIED BEFORE THIS TURN ──\n" +
		"These paths were dirty when the turn began; a status or diff mentioning them is not " +
		"evidence of this turn's work, and reverting them is not cleanup — it destroys somebody's " +
		"uncommitted changes:\n" + strings.Join(shown, ", ") + more
}

// preexistingDirtOf reads the turn-start capture for a session.
func (a *App) preexistingDirtOf(sid session.SessionID) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.preexistingDirt
	}
	return nil
}
