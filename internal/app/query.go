package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// WriteTool changes a file in the workspace on a person's behalf, and writes down that they did.
//
// # Why this is not ReadOnlyTool with a longer list
//
// Because of the second half. A console that could quietly put bytes in a workspace would be doing
// the agent's work without the agent's log, its approval policy, or its account of why — and the
// next turn would overwrite it, or build on it, with nothing anywhere saying a person had been in
// there. Orca and Paseo answer this with a worktree per agent and git as the record; magi has one
// workspace per companion and a log, so the log is where it goes.
//
// So the note is not decoration and not optional: it is appended as the PERSON's own words, in the
// session the companion is running, before this returns. It shows up in the transcript, in the
// interventions list, and — because a session log IS the context — in front of the model on its
// next turn. Nothing is started by it: editing a file is not asking for work, and a console that
// launched a turn because somebody saved would be a second surprise on top of the first.
//
// # What may be written
//
// Two tools, both of which take a path and confine it to the workspace exactly as the reading ones
// do. Not bash: a shell is not a file edit, however easily it can perform one, and a door named
// for editing that ran commands would be the sort of thing this tree keeps finding and closing.
func (a *App) WriteTool(ctx context.Context, sid session.SessionID, workdir, name string,
	args json.RawMessage, ask bool) (string, error) {
	if !writeTools[name] {
		return "", fmt.Errorf("%q is not a tool this can run: only %s", name, writeList())
	}
	t, ok := a.tools.Get(name)
	if !ok {
		return "", fmt.Errorf("%q is not a tool this companion has", name)
	}
	res, err := t.Execute(ctx, args, port.ToolEnv{Workdir: workdir, SessionID: sid})
	if err != nil {
		return "", err
	}
	if res.IsError {
		return "", fmt.Errorf("%s", toolText(res.Content))
	}
	if nerr := a.noteEdit(ctx, sid, name, args, ask); nerr != nil {
		// Said, not swallowed. The write happened; what failed is the part that makes it honest,
		// and a caller that thinks both halves succeeded would report a clean save.
		return toolText(res.Content), fmt.Errorf("the file was written and the note about it was not: %w", nerr)
	}
	return toolText(res.Content), nil
}

var writeTools = map[string]bool{"write": true, "edit": true}

func writeList() string {
	names := make([]string, 0, len(writeTools))
	for n := range writeTools {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// noteEdit puts the person's edit into the companion's own log.
//
// A user prompt, because that is what the rest of this system already means by "a person said
// something": the transcript draws it, Interventions counts it, and the next turn reads it. Not a
// fact of its own type — a new type would have to be taught to every one of those readers, and the
// three that matter would each learn it slightly differently.
//
// appendPrompt and not Submit: appending records, submitting also STARTS a turn.
func (a *App) noteEdit(ctx context.Context, sid session.SessionID, tool string, args json.RawMessage, ask bool) error {
	var where struct {
		Path string `json:"path"`
	}
	what := "a file in this workspace"
	// The arguments were good enough for the tool to run on, so a failure here says the shape
	// changed rather than that the caller was wrong — and the note is still worth writing with the
	// vaguer subject. Named rather than discarded, because a silently dropped error is how this
	// tree stops noticing that shape changed.
	if uerr := json.Unmarshal(args, &where); uerr == nil && strings.TrimSpace(where.Path) != "" {
		what = strings.TrimSpace(where.Path)
	}
	text := "I edited " + what + " from the console (" + tool + "). " +
		"Read it again before you change it — what you have in context is what it was before."
	return a.noteToSession(ctx, sid, text, ask)
}

// noteToSession puts a person's own words into a companion's log.
//
// ask says whether the companion should answer NOW. Off, this is a record: the sentence is in the
// context the next turn is built from and nothing runs. On, it is exactly a steer — the same act as
// typing it into the composer — and an idle companion picks it up immediately.
//
// Which of the two is the person's call and not this function's. A console that always answered
// would spend a turn on every click of a stage button; one that never did would make somebody who
// wants a second opinion on the edit they just made go and type "look at it".
//
// Shared by every console action that changes something under a running agent — an edit, and each
// of the git commands — because they all need the same two things: the sentence has to reach the
// context the next turn is built from, and it must not look like a request nobody answered.
//
// Written out rather than through appendPrompt, because the abandonment below has to NAME this
// prompt and appendPrompt keeps the id it minted to itself.
func (a *App) noteToSession(ctx context.Context, sid session.SessionID, text string, ask bool) error {
	msgID := "m_" + newSortableID()
	data, err := json.Marshal(event.PromptSubmittedData{
		MessageID: msgID,
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	if err != nil {
		return err
	}
	if aerr := a.appendFact(ctx, sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorUser}, data); aerr != nil {
		return aerr
	}
	// A prompt nobody is going to answer has to say so, or it is an open turn.
	//
	// Everything that reads a log decides "is this companion mid-turn" from the last prompt with no
	// finish after it — so a note appended while the companion was idle made the fleet row say
	// Working, and a stopped one say Abandoned. Neither happened. TypePromptAbandoned is exactly
	// this state and already exists for the cancelled-turn case: it closes the turn for every
	// reader that asks, and reconstruct ignores it, so the text stays in the context the next turn
	// is built from. Which is the whole point of writing it down.
	//
	// Only when nothing is running. Mid-turn, this IS an ordinary steer: the loop picks it up, the
	// turn that answers it is the one already going, and marking it abandoned would tell the loop
	// to skip what a person just said.
	if _, running := a.Running(); running {
		return nil
	}
	// Asked for an answer, and nothing is running: start the turn. Not marked abandoned, because
	// it is not abandoned — something is about to read it.
	if ask {
		a.startRun(ctx, sid)
		return nil
	}
	done, derr := json.Marshal(event.PromptAbandonedData{MsgID: msgID})
	if derr != nil {
		return derr
	}
	return a.appendFact(ctx, sid, event.TypePromptAbandoned,
		event.Actor{Kind: event.ActorSystem, ID: "console-edit"}, done)
}

// FileDo makes, moves and removes files in a workspace on a person's behalf.
//
// # Why this is not WriteTool with more names
//
// Writing a file is one act with one shape: a path and some bytes, which the agent's own write
// tool already does, confined and jailed. Making a directory, renaming a thing and deleting one
// are three different acts on the TREE, and the last of them cannot be undone by doing the
// opposite. They get their own door, their own confirmation upstairs, and — like every other
// change this console makes — a line in the companion's log saying what happened and what it
// means for what the agent is holding.
//
// # Delete is a delete, not a git rm
//
// The file goes; the index is left alone. A tracked file removed this way shows up as a change git
// can put back, which is the recovery every editor's delete relies on — and `git rm` would stage
// the removal, quietly making it part of a commit somebody else was about to make.
func (a *App) FileDo(ctx context.Context, sid session.SessionID, workdir, what, path, to string,
	ask bool) error {
	abs, err := insideWorkdir(workdir, path)
	if err != nil {
		return err
	}
	var note string
	switch what {
	case "new-file":
		if _, serr := os.Stat(abs); serr == nil {
			// Never over the top of something. A "new file" that silently opened an existing one
			// for overwriting is the shape of a lost afternoon.
			return fmt.Errorf("%s already exists", path)
		}
		if merr := os.MkdirAll(filepath.Dir(abs), 0o755); merr != nil {
			return merr
		}
		if werr := os.WriteFile(abs, nil, 0o644); werr != nil {
			return werr
		}
		note = "I made the file " + path + " from the console. It is empty."
	case "new-dir":
		if merr := os.MkdirAll(abs, 0o755); merr != nil {
			return merr
		}
		note = "I made the directory " + path + " from the console."
	case "rename":
		dest, derr := insideWorkdir(workdir, to)
		if derr != nil {
			return derr
		}
		if _, serr := os.Stat(dest); serr == nil {
			return fmt.Errorf("%s already exists", to)
		}
		if merr := os.MkdirAll(filepath.Dir(dest), 0o755); merr != nil {
			return merr
		}
		if rerr := os.Rename(abs, dest); rerr != nil {
			return rerr
		}
		note = "I renamed " + path + " to " + to + " from the console. Anything you have read from " +
			"the old path is at the new one now."
	case "delete":
		// One thing, not a tree. Removing a directory full of work with one press is not a file
		// manager's job on a screen with no undo — a directory goes when it is empty, which is the
		// same rule every editor's delete follows for the same reason.
		if rerr := os.Remove(abs); rerr != nil {
			return rerr
		}
		note = "I deleted " + path + " from the console. What you have in context is a file that " +
			"is no longer there; if it was tracked, git can put it back."
	default:
		return fmt.Errorf("%q is not one of the things this can do to a file: new-file, new-dir, rename, delete", what)
	}
	return a.noteToSession(ctx, sid, note, ask)
}

// insideWorkdir resolves a path against the workspace and refuses anything outside it.
//
// The same jail the tools apply, applied here because these calls do not go through a tool: a
// path is cleaned, joined, and checked against the workspace root — and the check is on the
// RESULT, so "../../etc/passwd" and a symlink pointing out are both refused rather than one of
// them being spotted by a rule about how the string looked.
func insideWorkdir(workdir, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("which file")
	}
	base, err := filepath.Abs(filepath.Clean(workdir))
	if err != nil {
		return "", err
	}
	abs := filepath.Clean(filepath.Join(base, path))
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	}
	rel, rerr := filepath.Rel(base, abs)
	if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside this workspace", path)
	}
	// And where the parent already exists, its REAL path has to be inside too: a symlink inside the
	// workspace pointing out of it is the case a lexical check cannot see.
	//
	// Both sides resolved, which is the part this got wrong first time. A workspace is very often
	// reached through a symlink — /tmp and /var are ones on macOS, and every t.TempDir() lives
	// under them — so comparing a resolved child against an unresolved base said that every file in
	// such a workspace was outside it. The check is real-path against real-path or it is a check
	// that refuses honest callers.
	realBase := base
	if r, lerr := filepath.EvalSymlinks(base); lerr == nil {
		realBase = r
	}
	if real, lerr := filepath.EvalSymlinks(filepath.Dir(abs)); lerr == nil {
		if rel, rerr := filepath.Rel(realBase, real); rerr != nil || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%s is outside this workspace", path)
		}
	}
	return abs, nil
}

// PatchFile applies a unified diff to a file in the workspace, on a person's behalf.
//
// # Why a patch and not the file
//
// A console save used to send the whole file. That is fine for a page of markdown and wrong for a
// source file of four thousand lines: the bytes go over a tunnel on every save, and — worse — the
// last writer wins. A patch carries only what changed, and it carries the CONTEXT of what changed,
// so applying it is also the check nobody had written: if the agent has edited that file since the
// person opened it, the context no longer matches and git refuses. A save that would have silently
// thrown away somebody's work becomes a sentence saying so.
//
// # Why git apply and not our own
//
// The same reason the diff is git's: whitespace rules, line endings, an incomplete last line. A
// patcher written here would be a second implementation that disagrees with the one the agent uses
// in exactly the cases nobody tests.
//
// A workspace that is not a checkout has no git to apply with, and the caller falls back to
// sending the file — which is honest, since there is nothing to conflict with either.
func (a *App) PatchFile(ctx context.Context, sid session.SessionID, workdir, path, patch string,
	ask bool) error {
	if a.plat == nil {
		return fmt.Errorf("platform unavailable")
	}
	if _, err := insideWorkdir(workdir, path); err != nil {
		return err
	}
	if strings.TrimSpace(patch) == "" {
		return fmt.Errorf("nothing to apply")
	}
	res, err := a.plat.Exec(ctx, port.Cmd{
		// --unidiff-zero because a patch made from two buffers may carry no context at all around a
		// change at the very start or end of a file; everywhere else the context is what makes the
		// refusal below meaningful.
		Path: "git", Args: []string{"apply", "--unidiff-zero", "--whitespace=nowarn", "-"},
		Dir: workdir, Stdin: []byte(patch), MaxOutput: 1 << 16,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		// git's own words, with the one thing git cannot know said in front of them: what this
		// almost always means here is that the file moved under the person while they were typing.
		return fmt.Errorf("%s changed since you opened it, so this could not be applied on top: %s",
			path, strings.TrimSpace(string(res.Stderr)))
	}
	return a.noteEdit(ctx, sid, "patch", []byte(`{"path":`+jsonString(path)+`}`), ask)
}

// jsonString is a Go string as a JSON literal, for the two places that build a small object by
// hand rather than marshalling a struct that exists for one line.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
