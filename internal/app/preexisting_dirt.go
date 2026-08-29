package app

import (
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

// preexistingDirtPaths lists the worktree paths that were already modified when the turn began.
// A GitFacts error (not a checkout, no git) is an empty list — nothing to disclaim.
func preexistingDirtPaths(st GitState, err error) []string {
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(st.Changes))
	for _, c := range st.Changes {
		out = append(out, c.Path)
	}
	return out
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
