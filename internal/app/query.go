package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Read-only query / inspection surface over sessions and the workspace: transcript and plan
// views, child-session listing, and git-diff / shell helpers used by the UI. Split out of
// app.go; behavior unchanged.

// Rewind removes the last n user turns from a session by truncating its event
// log, and clears derived per-session state. Returns the new highest seq.
func (a *App) Rewind(ctx context.Context, sid session.SessionID, n int) (int64, error) {
	if n < 1 {
		n = 1
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return 0, err
	}
	var promptSeqs []int64
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			promptSeqs = append(promptSeqs, e.Seq)
		}
	}
	if len(promptSeqs) == 0 {
		return 0, fmt.Errorf("nothing to rewind")
	}
	if n > len(promptSeqs) {
		n = len(promptSeqs)
	}
	boundary := promptSeqs[len(promptSeqs)-n] - 1 // keep everything before that prompt
	if err := a.store.Truncate(ctx, sid, boundary); err != nil {
		return 0, err
	}
	a.mu.Lock()
	if st, ok := a.stateIf(sid); ok {
		st.lastPromptTokens = 0
		st.todos = nil
	}
	a.mu.Unlock()
	return boundary, nil
}

// NewSince answers "has anything happened since seq?", and it is cheap.
//
// A viewer polls to find out whether anything happened, and the only way to ask used to be
// SessionState, which reads the whole log and rebuilds every message from it. Measured on a
// four-thousand-event session: 12.1ms to rebuild, 3.6µs to ask this — three thousand times the
// cost, paid two and a half times a second, per viewer, and on the overwhelming majority of ticks
// the answer is "nothing changed".
//
// Why the rebuild is not itself cached: the RENDERED transcript is not append-only even though the
// log is. A resurfaced interjection removes an earlier prompt from the display when a later event
// arrives, and a compaction rewrites the log outright — so a shared prefix would need an
// invalidation signal that does not exist, and getting it wrong shows a transcript the log no
// longer says. Asking first and rebuilding whole is the version that cannot drift.
func (a *App) NewSince(ctx context.Context, sid session.SessionID, seq int64) (latest int64, changed bool, err error) {
	// The store answers this from its own cache with a binary search, so the cost is the size of
	// the TAIL rather than of the log. Asking for everything and taking the last event would be
	// the same answer at two hundred times the price.
	evs, err := a.store.Read(ctx, sid, seq)
	if err != nil {
		return seq, false, err
	}
	if len(evs) == 0 {
		return seq, false, nil
	}
	return evs[len(evs)-1].Seq, true, nil
}

// SessionState returns a resumed session's reconstructed messages and the
// highest seq seen (so a UI can subscribe for only newer events).
func (a *App) SessionState(ctx context.Context, sid session.SessionID) ([]session.Message, int64, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil, 0, err
	}
	var last int64
	for _, e := range evs {
		if e.Seq > last {
			last = e.Seq
		}
	}
	a.restoreTodos(sid, evs)
	// Pair each re-surfaced queued interjection with its answer for display: drop the
	// stranded original so only the re-emitted copy (which sits next to its answer at
	// the back of the stream) renders. Display-only — turn logic uses reconstruct directly.
	return reconstruct(dropResurfacedOrigins(evs)), last, nil
}

// restoreTodos rebuilds a resumed session's plan from the log.
//
// The todos.changed facts were written for exactly this — their own comment calls them
// "logged, replayable" — and nothing had ever read one back. Todos() answers from
// sessionState, which a freshly started process has never filled for a session it did not
// run, so a resumed session came up with no plan at all: the panel was empty and, worse,
// prompt.go dropped the plan block from the agent's own context, with every step of it
// sitting in the log this function is already holding.
//
// The last fact wins (it is the whole plan after that change, not a delta), and an
// in-memory plan is never overwritten: the live session is the newer truth, and switching
// away from a running session and back must not roll its progress backwards.
func (a *App) restoreTodos(sid session.SessionID, evs []event.Event) {
	var td []session.Todo
	found := false
	for _, e := range evs {
		if e.Type != event.TypeTodosChanged {
			continue
		}
		var d event.TodosChangedData
		if json.Unmarshal(e.Data, &d) == nil {
			td, found = d.Todos, true
		}
	}
	if !found {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if st := a.stateLocked(sid); len(st.todos) == 0 {
		st.todos = td
	}
}

// CouncilMemberNames returns the configured council seats' names in order — the
// MAGI defaults when none are configured. Display-only (the splash nameplates);
// the gate resolves members itself so a config change cannot skew a running vote.
func (a *App) CouncilMemberNames() []string {
	ms := a.cfg.CouncilMembers
	if len(ms) == 0 {
		ms = council.DefaultMembers()
	}
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	return names
}

// Todos returns a session's current plan.
func (a *App) Todos(sid session.SessionID) []session.Todo {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.todos
	}
	return nil
}

// GitDiff returns the complete working-tree diff for workdir (empty if no
// changes), INCLUDING the content of new untracked files. A plain `git diff`
// omits untracked files, which hides exactly the new files an agent most often
// creates — and starves the council (the termination gate) of the evidence it
// needs to confirm the work, so it keeps voting "continue". To include them
// without disturbing the user's index, everything is staged into a throwaway
// index (GIT_INDEX_FILE) and diffed against HEAD; the real index is untouched.
func (a *App) GitDiff(ctx context.Context, workdir string) (string, error) {
	if a.plat == nil {
		return "", fmt.Errorf("platform unavailable")
	}

	// Complete diff via a throwaway index, so new files show up with content.
	if idx, err := os.CreateTemp("", "magi-diff-index-*"); err == nil {
		idxPath := idx.Name()
		idx.Close()
		os.Remove(idxPath) // git recreates it; we only needed a unique, unused path
		defer os.Remove(idxPath)
		env := []string{"GIT_INDEX_FILE=" + idxPath}
		add, aerr := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"add", "-A"}, Dir: workdir, Env: env})
		if aerr == nil && add.ExitCode == 0 {
			// HEAD when there is history, the empty-tree object otherwise (fresh repo
			// with no commits), so every staged file shows as an addition.
			against := "HEAD"
			if rev, rerr := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"rev-parse", "--verify", "-q", "HEAD"}, Dir: workdir}); rerr != nil || rev.ExitCode != 0 {
				against = emptyTreeRef
			}
			diff, derr := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"diff", "--cached", against}, Dir: workdir, Env: env})
			if derr == nil && diff.ExitCode == 0 {
				return string(diff.Stdout), nil
			}
		}
	}

	// Fallback (temp index unavailable, or not a git repo): plain working-tree
	// diff, then a status summary if the diff is empty but untracked files exist.
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"diff"}, Dir: workdir})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = "git diff failed"
		}
		return "", fmt.Errorf("%s", msg)
	}
	if strings.TrimSpace(string(res.Stdout)) != "" {
		return string(res.Stdout), nil
	}
	st, err := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: []string{"status", "--short"}, Dir: workdir})
	if err == nil && strings.TrimSpace(string(st.Stdout)) != "" {
		return string(st.Stdout), nil
	}
	return "", nil
}

// RunShell executes a one-shot shell command in workdir and returns its combined
// stdout+stderr and exit code. It backs the TUI's `!`-prefixed inline shell: the
// user typed the command explicitly, so it runs immediately, with no permission
// gate (unlike the agent's bash tool). Synchronous and foreground — for long-lived
// processes the agent's bash background mode is the right path, not this.
func (a *App) RunShell(ctx context.Context, workdir, cmd string) (out string, exit int, err error) {
	if a.plat == nil {
		return "", -1, fmt.Errorf("platform unavailable")
	}
	// Cap capture at the source so an unbounded producer (`!yes`, `!cat /dev/zero`)
	// can't grow the buffer to OOM before the caller trims it for display.
	res, e := a.plat.Exec(ctx, port.Cmd{Path: "/bin/sh", Args: []string{"-c", cmd}, Dir: workdir, MaxOutput: shellCaptureCap})
	if e != nil {
		return "", -1, e
	}
	return string(res.Stdout) + string(res.Stderr), res.ExitCode, nil
}

// ListSessions returns session metadata for a workdir.
func (a *App) ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error) {
	return a.store.ListSessions(ctx, workdir)
}
