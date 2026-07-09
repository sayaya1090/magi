package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

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
		st.stage = ""
		st.criteria = ""
		st.estSteps = 0
	}
	a.mu.Unlock()
	return boundary, nil
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
	// Pair each re-surfaced queued interjection with its answer for display: drop the
	// stranded original so only the re-emitted copy (which sits next to its answer at
	// the back of the stream) renders. Display-only — turn logic uses reconstruct directly.
	return reconstruct(dropResurfacedOrigins(evs)), last, nil
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

// PlanChildren returns the child sessions spawned to carry out the given plan step
// of parent, in creation order. It joins the parent link (session.Parent) with the
// per-child ParentStep edge recorded at spawn — the pair reconstructs the plan tree
// so a child's own todos can render indented under this step. Empty when the step was
// solo or its delegate/refine child never registered a sub-plan.
func (a *App) PlanChildren(parent session.SessionID, step int) []session.SessionID {
	a.mu.Lock()
	defer a.mu.Unlock()
	var kids []session.Session
	for _, st := range a.states {
		s := st.meta
		if s.Parent == parent && s.ParentStep != nil && *s.ParentStep == step {
			kids = append(kids, s)
		}
	}
	sort.Slice(kids, func(i, j int) bool {
		if !kids[i].Created.Equal(kids[j].Created) {
			return kids[i].Created.Before(kids[j].Created)
		}
		return kids[i].ID < kids[j].ID // stable tie-break for same-instant spawns
	})
	out := make([]session.SessionID, len(kids))
	for i, s := range kids {
		out[i] = s.ID
	}
	return out
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

// ChildView is a finished subagent's restored transcript (for resuming a parent
// session's subagent panes).
type ChildView struct {
	ID       session.SessionID
	Role     string
	Messages []session.Message
}

// ChildSessions returns the subagent sessions spawned by parent, each with its
// reconstructed transcript, so a UI can restore them as (done) panes on resume.
func (a *App) ChildSessions(ctx context.Context, workdir string, parent session.SessionID) ([]ChildView, error) {
	metas, err := a.store.ChildSessions(ctx, workdir, string(parent))
	if err != nil {
		return nil, err
	}
	out := make([]ChildView, 0, len(metas))
	for _, m := range metas {
		msgs, _, err := a.SessionState(ctx, m.ID)
		if err != nil {
			continue
		}
		out = append(out, ChildView{ID: m.ID, Role: m.Agent, Messages: msgs})
	}
	return out, nil
}
