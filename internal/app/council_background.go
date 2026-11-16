package app

import (
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

// runningBackground lists the calls whose background jobs this conversation started and that have
// not finished yet (backgroundOwned is cleared when a job's completion is observed).
func (a *App) runningBackground(sid session.SessionID) []string {
	prefix := string(sid) + "/"
	a.backgroundMu.Lock()
	defer a.backgroundMu.Unlock()
	var ids []string
	for k := range a.backgroundOwned {
		if strings.HasPrefix(k, prefix) {
			ids = append(ids, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(ids)
	return ids
}

// backgroundWaitReason is why a turn ends without the council: the work it would judge is still
// running. The council judges results, and these do not exist yet; the note the finished job brings
// back (background_wake.go) opens the turn where the agent declares and the council convenes.
func backgroundWaitReason(ids []string) string {
	return "waiting on background work still running (" + strings.Join(ids, ", ") +
		"); the council convenes when it has finished"
}

// councilWaitAdvice is what a declaration made while that work runs is told instead of a verdict.
func councilWaitAdvice(ids []string) string {
	return "Council was not convened: background work this conversation started is still running (" +
		strings.Join(ids, ", ") + "), and the council judges results that do not exist yet. This turn ends now, " +
		"waiting, so the person can reach you. When the job ends, a \"[background job finished]\" note opens a new " +
		"turn: read the result there, finish the work, and declare completion then. Do not wait for it in this turn."
}
