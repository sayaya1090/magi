package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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

// ListSessions returns session metadata for a workdir. Top-level only — a child is listed under
// the session that spawned it, by ChildSessions.
func (a *App) ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error) {
	return a.store.ListSessions(ctx, workdir)
}

// ChildSessions returns the subagent sessions a session spawned.
//
// A child records its parent when it is created, so this is a fact of the log rather than a
// register somebody has to keep in step. Which also means it answers for a companion that has
// stopped, and for last week — neither of which the daemon's in-memory list of running children
// can do, and both of which are when somebody asks what a subagent actually did.
func (a *App) ChildSessions(ctx context.Context, workdir, parentID string) ([]session.SessionMeta, error) {
	return a.store.ChildSessions(ctx, workdir, parentID)
}

// ReadOnlyTool runs one of this app's read-only tools outside any turn.
//
// # Why the app and not the caller
//
// The tools live here, behind an unexported registry, and the environment they need — where the
// workspace is, what a session id means — is this type's business. A caller assembling a ToolEnv
// would be a second place that decides what a tool runs against.
//
// # The allowlist is here too
//
// Named tools only, and only ones that look. A caller that could name `bash` would have a shell in
// somebody's workspace through a door built for reading a file, and the refusal has to be made by
// the process that owns the workspace rather than by whatever was in front of it — there is more
// than one thing in front of it (a console, a relay, a peer) and only one of these.
//
// The environment is deliberately bare: no spawn, no scratch directories, no progress channel.
// Anything needing those is not a tool that only looks, and would fail here rather than quietly
// doing half of what it does inside a turn.
func (a *App) ReadOnlyTool(ctx context.Context, workdir, name string, args json.RawMessage) (string, error) {
	if !readOnlyTools[name] {
		return "", fmt.Errorf("%q is not a tool this can run: only %s", name, readOnlyList())
	}
	t, ok := a.tools.Get(name)
	if !ok {
		return "", fmt.Errorf("%q is not a tool this companion has", name)
	}
	res, err := t.Execute(ctx, args, port.ToolEnv{Workdir: workdir})
	if err != nil {
		return "", err
	}
	if res.IsError {
		// The tool's own words. A file that is not there, a path outside the workspace, a
		// directory that cannot be read — the tool has already said which, in the sentence the
		// agent would have got, and rewriting it here would give the console a second vocabulary
		// for the same failures.
		return "", fmt.Errorf("%s", toolText(res.Content))
	}
	return toolText(res.Content), nil
}

// toolText is a tool result as text. The payload is JSON — a quoted string for the ones that
// produce text — so a caller that passed it through raw would show a reader their file wrapped in
// quotes with every newline spelled out.
func toolText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// readOnlyTools is the whole of what may be run this way.
//
// Four, and each one only looks: what is in this directory (list), what matches this pattern
// (glob), what is in this file (read), where does this text appear (grep). Nothing here writes,
// runs, spawns, or asks a person anything.
var readOnlyTools = map[string]bool{"list": true, "glob": true, "read": true, "grep": true}

func readOnlyList() string {
	names := make([]string, 0, len(readOnlyTools))
	for n := range readOnlyTools {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
