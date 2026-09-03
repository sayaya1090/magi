// The doors — the methods that answer with one Response.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// jobTailBytes is how much of a background command's output rides on one reply. The strip shows a
// tail, so more could not be displayed; the cap is what keeps a runaway log off the socket four
// times a second.
const jobTailBytes = 8 << 10

// Event turns a pending prompt back into the event a UI already knows how to draw.
//
// A viewer in another process cannot receive the original: it is transient, published to the bus of
// the daemon that is blocked, and it never leaves that process. So the request's fields cross the
// wire and this rebuilds the same payload on the other side — the same call id above all, because
// that is what an answer is addressed to. A prompt rendered from a summary is one nobody can reply
// to.
//
// It lives here, next to the wire type it converts, rather than in whichever client happens to need
// it: a second client would otherwise write its own version, and a viewer that filled in three of
// the four fields would show a prompt that looks right and answers nothing.
func (w *Waiting) Event(sid session.SessionID) (event.Event, error) {
	if w == nil {
		return event.Event{}, errors.New("daemon: no pending prompt to draw")
	}
	var (
		typ  event.Type
		data []byte
		err  error
	)
	switch w.Kind {
	case "question":
		typ = event.TypeQuestionRequested
		data, err = json.Marshal(event.QuestionRequestedData{
			CallID: w.ID, Question: w.What, Options: w.Options, Report: w.Report,
			Index: w.Index, Total: w.Total})
	default:
		typ = event.TypePermissionRequested
		data, err = json.Marshal(event.PermissionRequestedData{
			CallID: w.ID, Name: w.What, Args: w.Args, Reason: w.Reason, Diff: w.Diff})
	}
	if err != nil {
		return event.Event{}, fmt.Errorf("daemon: rebuilding the prompt: %w", err)
	}
	return event.Event{
		SessionID: sid, Type: typ, Data: data,
		Actor: event.Actor{Kind: event.ActorSystem, ID: "daemon"},
	}, nil
}

// The methods that answer with something, one function each.
//
// serveConn used to hold all of them inline: nineteen blocks of `if req.Method == …`, each ending
// in the same four lines that write the response and continue. That trailer was the bug surface —
// a block that forgot it would answer nothing and then read the next request as though it had —
// and 466 lines in one function is a place where the next method gets added by copying the one
// above it.
//
// The three that are NOT here are the three that do more than answer: shutdown replies and then
// stops the daemon, and watch and transcript each give the connection over to a stream and never
// return to the loop.
// door is one method this daemon answers, and everything a caller has to know about it.
//
// It is one struct because these three facts were three places: the name→function table, the
// `eng.(Reviewer)` assertion inside the function, and capsOf. Adding `children` meant editing all
// three, and capsOf's own comment already names what happens when one is forgotten — "the code ran
// ahead of the advertisement, and nothing failed". capsOf is now derived from this table, so that
// particular silence is not available any more.
//
// The gate itself stays in the function body. Moving it here and asserting bare inside would make
// every body safe only BECAUSE dispatch ran first — tests call these directly, and a panic inside a
// daemon takes the connection with it. What is here instead is the DECLARATION, and a guard calls
// each door with an engine that implements nothing to check that the declaration is true.
type door struct {
	run func(context.Context, Engine, Request) Response
	// needs is a nil pointer to the interface this door requires — (*Reviewer)(nil) — or nil for a
	// door that answers either way with less in it (jobs, models, tools, status): those return an
	// OK with empty fields rather than a refusal, which is a different contract and not a missing
	// gate.
	//
	// A pointer because that is the only way to carry an interface TYPE as a value, and the type
	// is the point: a guard reads it back and compares it with the assertion the body actually
	// makes. Written as a predicate instead, a gate naming the wrong interface would agree with
	// the body on every engine that implements neither — which is every test engine there is, so
	// nothing would have failed.
	needs any
	// why is what run answers when needs is false. Held here so the table reads as the index of
	// every refusal this daemon can give, and checked against the real answer by a guard.
	why string
	// cap is the capability name that advertises this door in `about`. Empty means the door is
	// deliberately unadvertised, and noCap has to say why.
	cap string
}

// noCap names every gated door that is not advertised, and why. A guard fails if a door is in
// neither this map nor carrying a cap, so the next door cannot be silently forgotten the way the
// twenty here were.
var noCap = map[string]string{
	"about":      isTheAnswer,
	"hand":       onPress,
	"hand-state": onPress,
	"tool":       onPress,
	"edit-file":  onPress,
	"file-do":    onPress,
	"git":        onPress,
	"git-diff":   onPress,
	"git-do":     onPress,
	"git-msg":    onPress,
	"git-pr":     onPress,
	"pr-facts":   onPress,
	"pr-msg":     onPress,
	"look-over":  onPress,
	"complete":   onPress,
	"open-file":  onPress,
	"suggest":    onPress,
	"shell":      onPress,
	"meet":       onPress,
	"meet-join":  onPress,
}

// Filled in init rather than at declaration: capsOf reads this table, answerAbout calls capsOf,
// and the table names answerAbout — a cycle the compiler refuses at package level even though
// nothing runs until a request arrives. Breaking it here rather than by giving capsOf its own copy
// of the list, which is the duplicate this change exists to delete.
// can reports whether this engine satisfies the door's declared gate.
func (d door) can(e Engine) bool {
	if d.needs == nil {
		return true
	}
	// capsOf(nil) is a real call — it answers what a build speaks before any engine is in hand —
	// and a nil interface has no type to ask. It implements nothing, which is the honest answer.
	t := reflect.TypeOf(e)
	return t != nil && t.Implements(reflect.TypeOf(d.needs).Elem())
}

var answers map[string]door

func init() {
	answers = map[string]door{
		// Four that answer either way, with less in them when the engine cannot fill the fields. Not
		// an oversight: a screen drawing a status line wants the line, not a refusal it has to hide.
		"status": {run: answerStatus},
		"models": {run: answerModels},
		"tools":  {run: answerTools},
		"jobs":   {run: answerJobs},

		"about":      {run: answerAbout, needs: (*Describer)(nil), why: "this daemon cannot describe its companion"},
		"hand":       {run: answerHand, needs: (*Taker)(nil), why: "this daemon cannot be handed work"},
		"hand-state": {run: answerHand, needs: (*Taker)(nil), why: "this daemon cannot be handed work"},
		"tool":       {run: answerTool, needs: (*ToolReader)(nil), why: "this daemon cannot read its workspace"},
		"edit-file":  {run: answerEditFile, needs: (*ToolWriter)(nil), why: "this daemon cannot be asked to edit its workspace"},
		"file-do":    {run: answerFileDo, needs: (*FileKeeper)(nil), why: "this daemon cannot make or remove files"},
		"git":        {run: answerGit, needs: (*GitTeller)(nil), why: "this daemon cannot say what git makes of its workspace"},
		"git-diff":   {run: answerGitDiff, needs: (*GitTeller)(nil), why: "this daemon cannot show a diff"},
		"git-do":     {run: answerGitDo, needs: (*GitDoer)(nil), why: "this daemon cannot run git commands"},
		"meet-join":  {run: answerMeetJoin, needs: (*Speaker)(nil), why: "this daemon cannot take part in a meeting"},
		"meet":       {run: answerMeet, needs: (*Speaker)(nil), why: "this daemon cannot take part in a meeting"},
		"git-pr":     {run: answerGitPR, needs: (*Reviewer)(nil), why: "this daemon cannot open a pull request"},
		"pr-facts":   {run: answerPRFacts, needs: (*Reviewer)(nil), why: "this daemon cannot answer about pull requests"},
		"pr-msg":     {run: answerPRFacts, needs: (*Reviewer)(nil), why: "this daemon cannot answer about pull requests"},
		"git-msg":    {run: answerGitMsg, needs: (*Reviewer)(nil), why: "this daemon cannot draft a commit message"},
		"look-over":  {run: answerLookOver, needs: (*Reviewer)(nil), why: "this daemon cannot look over a file"},
		"complete":   {run: answerComplete, needs: (*Reviewer)(nil), why: "this daemon cannot complete code"},
		"open-file":  {run: answerOpenFile, needs: (*Reviewer)(nil), why: "this daemon cannot track an open file"},
		"suggest":    {run: answerSuggest, needs: (*Reviewer)(nil), why: "this daemon cannot suggest a prompt"},
		"shell":      {run: answerShell, needs: (*ShellRunner)(nil), why: "this daemon cannot run commands"},

		// Advertised. Each of these decides whether a screen exists, which is what the handshake is for.
		"config-get":  {run: answerConfigGet, needs: (*ConfigKeeper)(nil), why: "this daemon cannot read out its settings", cap: "settings"},
		"config-set":  {run: answerConfigSet, needs: (*ConfigKeeper)(nil), why: "this daemon cannot change its settings", cap: "settings"},
		"profiles":    {run: answerProfiles, needs: (*ConfigKeeper)(nil), why: "this daemon cannot list its backends", cap: "settings"},
		"mcp-attach":  {run: answerMCPAttach, needs: (*ToolServerHost)(nil), why: "this daemon cannot attach tool servers", cap: "tool-servers"},
		"mcp-detach":  {run: answerMCPDetach, needs: (*ToolServerHost)(nil), why: "this daemon cannot attach tool servers", cap: "tool-servers"},
		"sessions":    {run: answerSessions, needs: (*ConversationKeeper)(nil), why: "this daemon cannot list its conversations", cap: "sessions"},
		"session-new": {run: answerSessionNew, needs: (*ConversationKeeper)(nil), why: "this daemon cannot open a new conversation", cap: "session-new"},
		"children":    {run: answerChildren, needs: (*ChildLister)(nil), why: "this daemon cannot list a conversation's subagents", cap: "children"},
		"cron":        {run: answerCron, needs: (*CronTeller)(nil), why: "this daemon cannot read its schedule", cap: "cron"},
		"cron-set":    {run: answerCronEdit, needs: (*CronEditor)(nil), why: "this daemon cannot change its schedule", cap: "cron-set"},
		"cron-remove": {run: answerCronEdit, needs: (*CronEditor)(nil), why: "this daemon cannot change its schedule", cap: "cron-remove"},
		"job-kill":    {run: answerJobKill, needs: (*JobKiller)(nil), why: "this daemon cannot stop background commands", cap: "job-kill"},
	}
}

// answerJobKill stops one background command. Removed answers whether the id named a job — a ✕
// pressed twice must read "already gone", not "failure".
func answerJobKill(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(JobKiller)
	if !ok {
		return Response{Err: "this daemon cannot stop background commands"}
	}
	if strings.TrimSpace(req.Name) == "" {
		return Response{Err: "no job named"}
	}
	return Response{OK: true, Removed: k.KillBackgroundJob(req.Name)}
}

// answerCron reads the standing schedule out: broken jobs first (they are the ones nobody else
// mentions), then soonest first — the same order the TUI's own panel draws.
func answerCron(ctx context.Context, eng Engine, req Request) Response {
	t, ok := eng.(CronTeller)
	if !ok {
		return Response{Err: "this daemon cannot read its schedule"}
	}
	jobs := t.ScheduledHere()
	sort.SliceStable(jobs, func(i, k int) bool {
		if (jobs[i].Problem != "") != (jobs[k].Problem != "") {
			return jobs[i].Problem != "" // broken first: nothing else will mention them again
		}
		// Then the runnable, soonest first. A job with no next instant and no problem is switched
		// OFF — it belongs after everything that will actually run, not (as zero-before-anything
		// would put it) ahead of the whole schedule.
		ni, nk := !jobs[i].Next.IsZero(), !jobs[k].Next.IsZero()
		if ni != nk {
			return ni
		}
		return jobs[i].Next.Before(jobs[k].Next)
	})
	rows := make([]CronRow, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, cronRowOf(j))
	}
	return Response{OK: true, Cron: rows}
}

// cronRowOf renders one job for the wire. One place, because the edit doors answer with the new
// listing and a second renderer is a second chance for the two answers to disagree about the same
// job — which is exactly what a screen redrawing after its own edit would show.
func cronRowOf(j app.ScheduledJobInfo) CronRow {
	r := CronRow{Name: j.Name, Schedule: j.Schedule, Enabled: j.Enabled,
		Problem: j.Problem, Prompt: j.Prompt, Command: j.Command, Timeout: j.Timeout}
	if !j.Next.IsZero() {
		r.Next = j.Next.UTC().Format(time.RFC3339)
	}
	return r
}

// answerCronEdit writes one job and answers with the WHOLE new listing.
//
// The listing rather than an ok, because the caller that just edited is about to redraw and a
// second round trip is a second chance for the two answers to disagree. It is also how this
// handler tells a refusal from a success without reading prose: the engine reports both as a
// message with a nil error (its words are written for an agent), so what settles it is the fact —
// is the job there now, or gone. A message that did not change the world is a refusal, and it is
// handed back verbatim because it says why.
func answerCronEdit(ctx context.Context, eng Engine, req Request) Response {
	ed, ok := eng.(CronEditor)
	if !ok {
		return Response{Err: "this daemon cannot change its schedule"}
	}
	teller, ok := eng.(CronTeller)
	if !ok {
		// Without the listing this door cannot tell a refusal from a success, and answering
		// "probably fine" is the one thing a write door must never do.
		return Response{Err: "this daemon can change its schedule but cannot read it back"}
	}
	remove := req.Method == "cron-remove"
	c := CronEdit{Name: strings.TrimSpace(req.Name), Schedule: strings.TrimSpace(req.Schedule),
		Prompt: req.Text, Command: strings.TrimSpace(req.Command),
		Timeout: strings.TrimSpace(req.Timeout), Enabled: req.Enabled, Remove: remove}
	if c.Name == "" {
		return Response{Err: "which job — a job is named, and the name is how it is found again"}
	}
	msg, err := ed.EditCron(c)
	if err != nil {
		return Response{Err: err.Error()}
	}
	rows := make([]CronRow, 0)
	there := false
	for _, j := range teller.ScheduledHere() {
		rows = append(rows, cronRowOf(j))
		if j.Name == c.Name {
			there = true
		}
	}
	if there == remove {
		// Asked to write and it is not there, or asked to remove and it still is: the engine
		// refused, and its message is the reason.
		return Response{Err: strings.TrimSpace(msg)}
	}
	return Response{OK: true, Out: strings.TrimSpace(msg), Cron: rows}
}

// answerSessions lists this workspace's conversations, newest activity first. Whether a turn is
// OPEN in one is deliberately not here: answering it costs reading every log whole, and the one
// conversation it could matter for is the current one — which the caller already has from the
// roster row and can ask status about.
func answerSessions(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(ConversationKeeper)
	if !ok {
		return Response{Err: "this daemon cannot list its conversations"}
	}
	metas, err := k.SessionsHere(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	sort.SliceStable(metas, func(i, j int) bool { return metas[i].LastActivity.After(metas[j].LastActivity) })
	rows := make([]SessionRow, 0, len(metas))
	for _, m := range metas {
		r := SessionRow{ID: string(m.ID), Title: m.Title, Model: m.Model, Labels: m.Labels}
		if !m.Created.IsZero() {
			r.Created = m.Created.UTC().Format(time.RFC3339)
		}
		if !m.LastActivity.IsZero() {
			r.LastActivity = m.LastActivity.UTC().Format(time.RFC3339)
		}
		rows = append(rows, r)
	}
	return Response{OK: true, Sessions: rows}
}

// answerChildren lists the subagent conversations a session spawned, newest activity first.
//
// The parent is REQUIRED and refused when empty. A "current conversation" default was the
// obvious convenience and the wrong one: the current is a per-caller fact (each client holds
// its own idea of where it is), so a door that silently substituted it would answer a
// different question depending on who asked — and the caller that most wants this door is
// looking at a session that is NOT the current one (a finished child, an old meeting).
//
// An unknown id answers an empty list rather than an error, deliberately: a parent that spawned
// nothing and an id that never existed look the same in the log, and inventing a distinction
// here would mean scanning to prove absence. `sessions` is where an id is checked.
func answerChildren(ctx context.Context, eng Engine, req Request) Response {
	l, ok := eng.(ChildLister)
	if !ok {
		return Response{Err: "this daemon cannot list a conversation's subagents"}
	}
	parent := strings.TrimSpace(req.Session)
	if parent == "" {
		return Response{Err: "children needs the session whose subagents you want — `sessions` lists them"}
	}
	metas, err := l.ChildrenOf(ctx, parent)
	if err != nil {
		return Response{Err: err.Error()}
	}
	sort.SliceStable(metas, func(i, j int) bool { return metas[i].LastActivity.After(metas[j].LastActivity) })
	rows := make([]SessionRow, 0, len(metas))
	for _, m := range metas {
		r := SessionRow{ID: string(m.ID), Title: m.Title, Agent: m.Agent, Origin: m.Origin,
			Model: m.Model, Labels: m.Labels}
		if !m.Created.IsZero() {
			r.Created = m.Created.UTC().Format(time.RFC3339)
		}
		if !m.LastActivity.IsZero() {
			r.LastActivity = m.LastActivity.UTC().Format(time.RFC3339)
		}
		rows = append(rows, r)
	}
	// OK with an empty list, not an absent field with OK: a client must be able to tell "this
	// daemon answered, and there are none" from "this build has no such door" — which is what
	// the capability handshake is for, and what an omitted field would quietly undo.
	return Response{OK: true, Children: rows}
}

// answerSessionNew opens a fresh conversation and answers with its id — see ConversationKeeper
// for why creating and moving are one verb.
func answerSessionNew(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(ConversationKeeper)
	if !ok {
		return Response{Err: "this daemon cannot open a new conversation"}
	}
	sid, err := k.NewSession(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Session: string(sid)}
}

// answerConfigGet reads the editable settings.
func answerConfigGet(ctx context.Context, eng Engine, _ Request) Response {
	k, ok := eng.(ConfigKeeper)
	if !ok {
		return Response{Err: "this daemon cannot read out its settings"}
	}
	items, err := k.ConfigHere(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Config: items}
}

// answerConfigSet changes one setting and answers with the key as it now stands — so a screen
// redraws from the daemon's own reading rather than from what it hoped the write did.
func answerConfigSet(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(ConfigKeeper)
	if !ok {
		return Response{Err: "this daemon cannot change its settings"}
	}
	item, err := k.ConfigSet(ctx, req.Name, req.Text, req.Tier)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Config: []ConfigItem{item}}
}

// answerProfiles lists what a profile-shaped setting may point at.
func answerProfiles(ctx context.Context, eng Engine, _ Request) Response {
	k, ok := eng.(ConfigKeeper)
	if !ok {
		return Response{Err: "this daemon cannot list its backends"}
	}
	list, err := k.ProfilesHere(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Profiles: list}
}

// answerMCPAttach opens the runtime door: attach an HTTP MCP server to this companion.
//
// The reply carries the tool names rather than a bare ok. A caller told "ok" knows the handshake
// worked; a caller told mcp__ppt__render, mcp__ppt__open knows what it may now ask for; a caller
// told nothing at all knows the server answered and offers nothing. One ack flattens three
// different situations into one.
func answerMCPAttach(ctx context.Context, eng Engine, req Request) Response {
	h, ok := eng.(ToolServerHost)
	if !ok {
		return Response{Err: "this daemon cannot attach tool servers"}
	}
	names, err := h.AttachToolServer(ctx, req.Name, req.URL, req.Headers)
	if err != nil {
		return Response{Err: err.Error()}
	}
	// Never nil: a client reading JSON should see an empty list rather than null when a server
	// attached and advertised nothing.
	if names == nil {
		names = []string{}
	}
	return Response{OK: true, Tools: names}
}

// answerMCPDetach is the other half. A helper reconnecting after a crash sends detach first, and
// the answer tells it whether it was cleaning up or was already clean.
func answerMCPDetach(ctx context.Context, eng Engine, req Request) Response {
	h, ok := eng.(ToolServerHost)
	if !ok {
		return Response{Err: "this daemon cannot attach tool servers"}
	}
	removed, err := h.DetachToolServer(req.Name)
	if err != nil {
		return Response{Err: err.Error()} // refused: there is one, and it is not this caller's
	}
	return Response{OK: true, Removed: removed}
}

func answerStatus(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true}
	if ask, ok := eng.Waiting(session.SessionID(req.Session)); ok {
		resp.Waiting = &Waiting{
			ID: ask.ID, Kind: ask.Kind, What: ask.What, Args: ask.Args,
			Reason: ask.Reason, Options: ask.Options, Report: ask.Report,
			Diff:  ask.Diff,
			Index: ask.Index, Total: ask.Total,
			Since: ask.Since.UTC().Format(time.RFC3339),
		}
	}
	resp.Doing, _ = eng.Doing(session.SessionID(req.Session))
	if c, ok := eng.(Controller); ok {
		resp.Permission = c.Permission()
		resp.Backend = c.Backend()
	}
	if n, ok := eng.(UserNamer); ok {
		resp.User = n.UserLabel(session.SessionID(req.Session))
	}
	if m, ok := eng.(ModelNamer); ok {
		resp.Model = m.ModelOf(session.SessionID(req.Session))
	}
	return resp
}

// models is the list this companion could be put on, asked of its own backend.
func answerModels(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true, Models: []string{}}
	if l, ok := eng.(ModelLister); ok {
		// A backend that is slow or down must not hold the socket: the caller is a screen
		// drawing a menu, and no menu is a better answer than a stuck one.
		mctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		names, err := l.ListModels(mctx)
		cancel()
		if err == nil {
			resp.Models = names
		} else {
			resp.Why = err.Error()
		}
	}
	return resp
}

// tools is answered here too, and for the same reason.
func answerTools(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true, Tools: []string{}}
	if t, ok := eng.(ToolLister); ok {
		resp.Tools = t.ToolNames()
	}
	return resp
}

// jobs is answered here for the same reason status is: it carries a payload.
func answerJobs(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true, Jobs: &Jobs{}}
	if j, ok := eng.(JobRunner); ok {
		for _, b := range j.BackgroundJobs() {
			resp.Jobs.Background = append(resp.Jobs.Background, BackgroundJob{
				ID: b.ID, Command: b.Command, Running: b.Running, Killed: b.Killed,
				Exit: b.Exit, Started: b.Started.UTC().Format(time.RFC3339),
				Tail: j.BackgroundTail(b.ID, jobTailBytes),
			})
		}
		resp.Jobs.Queued = j.QueuedWork()
		for _, c := range j.SubagentJobs() {
			out := ChildJob{
				ID: c.ID, Tool: c.Tool, Task: c.Task, Running: c.Running,
				Steps: c.Steps, Err: c.Err,
				Started: c.Started.UTC().Format(time.RFC3339),
			}
			if !c.Ended.IsZero() {
				out.Ended = c.Ended.UTC().Format(time.RFC3339)
			}
			resp.Jobs.Children = append(resp.Jobs.Children, out)
		}
	}
	return resp
}

// about is answered here rather than in dispatch, like status and shell, because it has a
// payload. It is also the whole point of the relay: whoever is asking connected to THIS
// companion, so there is no name to resolve and no config directory to read — the process
// that knows answers about itself.
func answerAbout(ctx context.Context, eng Engine, req Request) Response {
	d, ok := eng.(Describer)
	if !ok {
		return Response{Err: "this daemon cannot describe its companion"}
	}
	// The rendered description as before, plus the structured handshake so a caller can negotiate:
	// the wire protocol and capabilities this build speaks, and — when the engine carries it — the
	// binary version. All additive and omitempty, so an older client that only reads Out is unaffected.
	resp := Response{OK: true, Out: d.About(), Proto: ProtoVersion, Caps: capsOf(eng)}
	if v, ok := eng.(Versioner); ok {
		resp.Version = v.Version()
	}
	return resp
}

// hand and hand-state are answered here for the same reason about is, and they are the
// reason the relay exists at all. Whoever is asking has connected to THIS companion, so
// there is no name to resolve against a config directory that may belong to another
// account and may not exist at all inside a container. The process doing the work says
// what became of it.
func answerHand(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	taker, ok := eng.(Taker)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot be handed work"}
	case req.Method == "hand":
		id, herr := taker.Hand(ctx, req.Name, req.Text, req.Looking)
		if herr != nil {
			resp = Response{Err: herr.Error()}
		} else {
			resp = Response{OK: true, Out: id}
		}
	default:
		h, herr := taker.Handed(ctx, req.Name)
		if herr != nil {
			resp = Response{Err: herr.Error()}
		} else {
			resp = Response{OK: true, Handover: &h}
		}
	}
	return resp
}

// A read-only tool, run where the workspace is. Answered here rather than in dispatch for the
// same reason shell is: it has a payload.
func answerTool(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	reader, ok := eng.(ToolReader)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot read its workspace"}
	case strings.TrimSpace(req.Name) == "":
		resp = Response{Err: "no tool named"}
	default:
		out, rerr := reader.ReadOnlyTool(ctx, req.Name, req.Args)
		if rerr != nil {
			resp = Response{Err: rerr.Error()}
		} else {
			resp = Response{OK: true, Out: out}
		}
	}
	return resp
}

// An edit, made where the workspace is, and written into the log as a person's own words.
func answerEditFile(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	writer, ok := eng.(ToolWriter)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot be asked to edit its workspace"}
	case strings.TrimSpace(req.Name) == "":
		resp = Response{Err: "no tool named"}
	case req.Name == "patch":
		if perr := writer.PatchFile(ctx, req.Text, req.Answer, req.Ask); perr != nil {
			resp = Response{Err: perr.Error()}
		} else {
			resp = Response{OK: true}
		}
	default:
		out, werr := writer.WriteTool(ctx, req.Name, req.Args, req.Ask)
		if werr != nil {
			resp = Response{Err: werr.Error()}
		} else {
			resp = Response{OK: true, Out: out}
		}
	}
	return resp
}

func answerGit(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	teller, ok := eng.(GitTeller)
	if !ok {
		resp = Response{Err: "this daemon cannot say what git makes of its workspace"}
	} else if out, gerr := teller.Git(ctx); gerr != nil {
		resp = Response{Err: gerr.Error()}
	} else {
		resp = Response{OK: true, Out: string(out)}
	}
	return resp
}

func answerFileDo(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	keeper, ok := eng.(FileKeeper)
	if !ok {
		resp = Response{Err: "this daemon cannot make or remove files"}
	} else if ferr := keeper.FileDo(ctx, req.Name, req.Text, req.Answer, req.Ask); ferr != nil {
		resp = Response{Err: ferr.Error()}
	} else {
		resp = Response{OK: true}
	}
	return resp
}

func answerGitDiff(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	teller, ok := eng.(GitTeller)
	if !ok {
		resp = Response{Err: "this daemon cannot show a diff"}
	} else if out, derr := teller.GitDiff(ctx, req.Text, req.Decision == "staged",
		req.Decision == "untracked"); derr != nil {
		resp = Response{Err: derr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerGitDo(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	doer, ok := eng.(GitDoer)
	if !ok {
		resp = Response{Err: "this daemon cannot run git commands"}
	} else if out, gerr := doer.GitDo(ctx, req.Name, req.Text, req.Answer, req.Ask); gerr != nil {
		resp = Response{Err: gerr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerMeetJoin(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	sp, ok := eng.(Speaker)
	if !ok {
		resp = Response{Err: "this daemon cannot take part in a meeting"}
	} else if ready, room, jerr := sp.MeetingJoin(ctx, req.Meeting, req.Name); jerr != nil {
		resp = Response{Err: jerr.Error()}
	} else {
		resp = Response{OK: true, Out: ready, Session: room}
	}
	return resp
}

func answerMeet(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	sp, ok := eng.(Speaker)
	if !ok {
		resp = Response{Err: "this daemon cannot take part in a meeting"}
	} else if c, merr := sp.MeetingTurn(ctx, req.Meeting, req.Name, req.Text,
		req.Decision == "closing"); merr != nil {
		resp = Response{Err: merr.Error()}
	} else {
		// A pass travels as a flag rather than as a word in the text, or a contribution
		// that happens to begin with the word would arrive as a silence.
		//
		// The room travels on every turn and not only on the join: a daemon that restarted
		// mid-meeting prepares again in a NEW session, and a viewer holding the old id
		// would show an empty working rather than the one that produced the sentence in
		// front of it.
		resp = Response{OK: true, Out: c.Said, Exit: passFlag(c.Pass), Session: c.Room}
	}
	return resp
}

func answerGitPR(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot open a pull request"}
	} else if url, perr := rev.OpenPR(ctx, req.Name, req.Text); perr != nil {
		resp = Response{Err: perr.Error()}
	} else {
		resp = Response{OK: true, Out: url}
	}
	return resp
}

func answerPRFacts(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot answer about pull requests"}
	} else {
		var out string
		var perr error
		if req.Method == "pr-facts" {
			out, perr = rev.PRFacts(ctx)
		} else {
			out, perr = rev.DraftPR(ctx, req.Text)
		}
		if perr != nil {
			resp = Response{Err: perr.Error()}
		} else {
			resp = Response{OK: true, Out: out}
		}
	}
	return resp
}

func answerGitMsg(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot draft a commit message"}
	} else if out, derr := rev.DraftCommit(ctx, req.Text); derr != nil {
		resp = Response{Err: derr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerLookOver(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot look over a file"}
	} else if out, lerr := rev.LookOver(ctx, req.Name, req.Text); lerr != nil {
		resp = Response{Err: lerr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerComplete(ctx context.Context, eng Engine, req Request) Response {
	rev, ok := eng.(Reviewer)
	if !ok {
		return Response{Err: "this daemon cannot complete code"}
	}
	var a completeArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &a); err != nil {
			return Response{Err: err.Error()}
		}
	}
	out, why, err := rev.CompleteCode(ctx, req.Name, a.Prefix, a.Suffix)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Out: out, Reason: string(why)}
}

func answerOpenFile(ctx context.Context, eng Engine, req Request) Response {
	rev, ok := eng.(Reviewer)
	if !ok {
		return Response{Err: "this daemon cannot track an open file"}
	}
	if err := rev.SetOpenFile(ctx, req.Name, req.Text); err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true}
}

func answerSuggest(ctx context.Context, eng Engine, req Request) Response {
	rev, ok := eng.(Reviewer)
	if !ok {
		return Response{Err: "this daemon cannot suggest a prompt"}
	}
	out, err := rev.SuggestPrompt(ctx, req.Text)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Out: out}
}

// shell is answered here rather than in dispatch, like status, because it has a payload:
// dispatch returns only an error, and giving it a return value for one caller would make
// every other write site pretend to produce something.
func answerShell(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	runner, ok := eng.(ShellRunner)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot run commands"}
	case strings.TrimSpace(req.Text) == "":
		resp = Response{Err: "no command"}
	default:
		out, code, rerr := runner.RunShellHere(ctx, req.Text)
		if rerr != nil {
			resp = Response{Err: rerr.Error()}
		} else {
			resp = Response{OK: true, Out: out, Exit: &code}
		}
	}
	return resp
}

// capsOf is Caps for a known engine, which is the only honest way to answer it.
//
// A capability that is a property of the BUILD can be a constant; "tool-servers" is not one. The
// door is an optional interface (ToolServerHost), so whether this daemon accepts mcp-attach is a
// fact about the engine it is running, and a build-level list would advertise it and then refuse —
// reintroducing, one layer down, exactly the distinction the capability exists to make. Two lines
// below, Version already asks the engine; this one did not.
//
// nil engine answers the build-level floor: what any daemon from this build speaks whoever is
// behind it.
func capsOf(eng Engine) []string {
	// "handshake" marks a build that answers this versioned about at all.
	//
	// "roster": this daemon answers who is out there (roster.go). Build-level, unlike everything
	// below: the answer is read from the listener's home directory, not from an engine interface,
	// so any daemon from this build speaks it. It is not in the door table for the same reason —
	// that table's shape is (ctx, Engine, Request).
	caps := []string{"handshake", "roster"}
	// "transcript": this daemon will read a conversation out down the socket. Not in the table
	// either: it turns the connection into a stream, so it is dispatched before the table is
	// consulted. Advertised for the reason every name below is — the clients this door exists for
	// have no log reader, and calling the method and reading a sentence back cannot tell a build
	// that does not know it from an engine that will not do it.
	if _, ok := eng.(Transcriber); ok {
		caps = append(caps, "transcript")
	}
	// The rest is read off the door table rather than written here a second time. That second copy
	// is what this function's own history is about: a door would land dispatched and unadvertised,
	// nothing would fail, and a client gated on the capability would never draw and never say why.
	// A name can still be wrong now, but it can no longer be MISSING.
	seen := map[string]bool{}
	for _, d := range answers {
		if d.cap == "" || seen[d.cap] || !d.can(eng) {
			continue
		}
		seen[d.cap] = true
		caps = append(caps, d.cap)
	}
	// Sorted from here down so the advertisement does not depend on map order — a client diffing
	// two daemons' caps, or a golden holding one, would otherwise see churn that means nothing.
	sort.Strings(caps[2:])
	return caps
}

// Filled in init for the reason `answers` is: acceptedMethods reads these tables, dispatchNow's
// refusal calls acceptedMethods, and the table names dispatchNow's functions.
func init() {
	// The conversation verbs. Every engine has them (they are on Engine itself), so no gate.
	say := func(steer bool) func(context.Context, Engine, Request, session.SessionID) error {
		return func(ctx context.Context, eng Engine, r Request, sid session.SessionID) error {
			p := command.SubmitPrompt{
				SessionID: sid,
				Parts:     []session.Part{{Kind: session.PartText, Text: r.Text}},
				Actor:     event.Actor{Kind: event.ActorUser, ID: "attach"},
				Refs:      r.Refs,
			}
			if steer {
				return conversationErr(sid, eng.Steer(ctx, p))
			}
			return conversationErr(sid, eng.Submit(ctx, p))
		}
	}
	// The five that change how the engine behaves. Five ENTRIES rather than one that fans out:
	// a table that knows four fewer names would need the hand-written list back to name them.
	//
	// Named apart from "permission", which ANSWERS a prompt. One word for "decide this call" and
	// "change the policy for every call" would be a wire that means two things.
	ctl := func(do func(context.Context, Controller, Request, session.SessionID) error) act {
		return act{
			needs: (*Controller)(nil),
			why:   "this daemon cannot be controlled remotely",
			run: func(ctx context.Context, eng Engine, r Request, sid session.SessionID) error {
				c, ok := eng.(Controller)
				if !ok {
					return fmt.Errorf("this daemon cannot be controlled remotely")
				}
				return do(ctx, c, r, sid)
			},
		}
	}
	acts = map[string]act{
		"submit": {run: say(false)},
		"steer":  {run: say(true)},
		"interrupt": {run: func(ctx context.Context, eng Engine, r Request, sid session.SessionID) error {
			return eng.Interrupt(ctx, command.Interrupt{SessionID: sid})
		}},
		"permission": {run: func(ctx context.Context, eng Engine, r Request, sid session.SessionID) error {
			return eng.RespondPermission(ctx, command.RespondPermission{
				SessionID: sid, CallID: r.CallID, Decision: r.Decision,
				Actor: event.Actor{Kind: event.ActorUser, ID: "attach"}})
		}},
		"answer": {run: func(ctx context.Context, eng Engine, r Request, sid session.SessionID) error {
			return eng.RespondQuestion(ctx, command.RespondQuestion{
				SessionID: sid, CallID: r.CallID, Answer: r.Answer})
		}},

		"rewind": ctl(func(ctx context.Context, c Controller, r Request, sid session.SessionID) error {
			_, err := c.Rewind(ctx, sid, r.N)
			return err
		}),
		"compact": ctl(func(ctx context.Context, c Controller, r Request, sid session.SessionID) error {
			return c.Compact(ctx, command.Compact{SessionID: sid})
		}),
		"set-model": ctl(func(_ context.Context, c Controller, r Request, sid session.SessionID) error {
			c.SetModel(sid, r.Name)
			return nil
		}),
		"use-backend": ctl(func(_ context.Context, c Controller, r Request, sid session.SessionID) error {
			return c.UseBackend(sid, r.Name)
		}),
		"set-permission": ctl(func(_ context.Context, c Controller, r Request, _ session.SessionID) error {
			c.SetPermission(r.Name)
			return nil
		}),

		"resume": {
			needs: (*SessionMover)(nil),
			why:   "this companion cannot be moved to another conversation",
			run: func(ctx context.Context, eng Engine, _ Request, sid session.SessionID) error {
				m, ok := eng.(SessionMover)
				if !ok {
					return fmt.Errorf("this companion cannot be moved to another conversation")
				}
				return m.Resume(ctx, sid)
			},
		},
		// Its own optional interface rather than another method on Controller. Controller is what
		// every fake in every package must satisfy, and this file already says that a control
		// surface which grows is not a reason to touch four test doubles.
		"reload-cron": {
			needs: (*CronController)(nil),
			why:   "this daemon holds no scheduled work",
			run: func(_ context.Context, eng Engine, _ Request, _ session.SessionID) error {
				c, ok := eng.(CronController)
				if !ok {
					return fmt.Errorf("this daemon holds no scheduled work")
				}
				c.ReloadCron()
				return nil
			},
		},
	}
}

func textOf(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind == session.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
