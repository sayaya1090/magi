package fleet_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/cluster"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// fleetFixture builds a viewer over a real store and a real config directory, so the test exercises
// the same two facts the dashboard derives everything from: whether a socket answers, and what the
// log says.
type fleetFixture struct {
	reader *app.App
	store  *jsonl.Store
	cfgDir string
	here   string
	t      *testing.T
}

// shortTempDir is t.TempDir() with a path a unix socket can live at.
//
// A unix address is about 104 bytes and t.TempDir() on macOS starts with /var/folders/…/T/ plus the
// test's own name — TestTargetAcceptsAnyListedDaemonAndNothingElse alone is 45 of them. Binding
// there fails with "invalid argument", which says nothing about length; observed exactly that way
// writing this test, which is also why daemon.tooLong exists.
func shortTempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "magiweb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func newFleetFixture(t *testing.T) *fleetFixture {
	t.Helper()
	cfg, data := shortTempDir(t), t.TempDir()
	st, err := jsonl.New(data)
	if err != nil {
		t.Fatal(err)
	}
	return &fleetFixture{
		reader: app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{}),
		store:  st,
		cfgDir: cfg,
		t:      t,
	}
}

// daemonAt publishes a record for a workspace, and listens on the socket when live is true. A dead
// daemon is exactly a record with nothing behind it — the crash case, which is the one the
// dashboard exists to make visible.
func (f *fleetFixture) daemonAt(workdir, sid string, live bool) string {
	f.t.Helper()
	// Short socket paths: the OS refuses long ones and a t.TempDir() under a long test name gets
	// close. Named by hand rather than via SocketPath for the same reason.
	sock := filepath.Join(f.cfgDir, "daemon-"+sid+".sock")
	if live {
		ln, err := net.Listen("unix", sock)
		if err != nil {
			f.t.Fatal(err)
		}
		f.t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
	} else if err := os.WriteFile(sock, nil, 0o600); err != nil {
		f.t.Fatal(err) // a socket file with nobody listening: what SIGKILL leaves behind
	}
	unpublish, err := daemon.Publish(sock, workdir, sid, daemon.Identity{})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(unpublish)
	return sock
}

// session writes a log: a prompt, some tool calls, and a turn.finished only if finished is true.
func (f *fleetFixture) session(sid, workdir, prompt string, steps int, finished bool) {
	f.t.Helper()
	ctx := context.Background()
	ev := func(t event.Type, d any) event.Event {
		b, err := json.Marshal(d)
		if err != nil {
			f.t.Fatal(err)
		}
		return event.Event{Type: t, Data: b, TS: time.Now()}
	}
	evs := []event.Event{
		ev(event.TypeSessionCreated, event.SessionCreatedData{Workdir: workdir}),
		ev(event.TypePromptSubmitted, event.PromptSubmittedData{
			MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: prompt}}}),
	}
	for i := 0; i < steps; i++ {
		evs = append(evs, ev(event.TypePartAppended, event.PartAppendedData{
			MessageID: "a1", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				CallID: "c", Name: "bash", Args: json.RawMessage(`{"command":"go build ./..."}`)}}}))
	}
	if finished {
		evs = append(evs, ev(event.TypeTurnFinished, event.TurnFinishedData{}))
	}
	if _, err := f.store.Append(ctx, session.SessionID(sid), evs...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fleetFixture) get() []fleet.Agent {
	f.t.Helper()
	list, err := fleet.List(context.Background(), f.reader, f.cfgDir, f.here)
	if err != nil {
		f.t.Fatal(err)
	}
	return list
}

func find(t *testing.T, list []fleet.Agent, sid string) fleet.Agent {
	t.Helper()
	for _, a := range list {
		if a.Session == sid {
			return a
		}
	}
	t.Fatalf("no agent with session %q in %+v", sid, list)
	return fleet.Agent{}
}

// The dashboard's whole claim is that it can tell four situations apart, and each of the four means
// something different to the person reading it. "abandoned" is the one that pays for the feature:
// a daemon that died mid-turn looks identical to a finished one in every other view.
func TestFleetTellsTheFourStatesApart(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "working1", true)
	f.session("working1", wd, "make the tests pass", 3, false)
	f.daemonAt(wd, "idle1", true)
	f.session("idle1", wd, "what does this do", 1, true)
	f.daemonAt(wd, "aband1", false)
	f.session("aband1", wd, "refactor the store", 7, false)
	f.daemonAt(wd, "stop1", false)
	f.session("stop1", wd, "done here", 2, true)

	list := f.get()
	if len(list) != 4 {
		t.Fatalf("expected 4 agents, got %d: %+v", len(list), list)
	}
	for sid, want := range map[string]fleet.State{
		"working1": fleet.Working, "idle1": fleet.Idle, "aband1": fleet.Abandoned, "stop1": fleet.Stopped,
	} {
		if got := find(t, list, sid).State; got != want {
			t.Errorf("session %s: state %q, want %q", sid, got, want)
		}
	}

	// A working agent carries what it is working ON and how far it has got. A card that said only
	// "working" would send you into every agent to find out which one is yours.
	w := find(t, list, "working1")
	if !strings.Contains(w.Task, "make the tests pass") {
		t.Errorf("working card does not say what the turn was: %q", w.Task)
	}
	if w.Steps != 3 {
		t.Errorf("working card reports %d steps, the log has 3", w.Steps)
	}
	// And an abandoned one says how much work was thrown away — the number that decides whether
	// you resume it.
	if a := find(t, list, "aband1"); a.Steps != 7 {
		t.Errorf("abandoned card reports %d steps, the log has 7", a.Steps)
	}
}

// The full workspace path is on the card. The socket NAME carries only a base name and a hash, so a
// dashboard built from filenames alone cannot tell two checkouts of the same repo apart — which is
// the exact case where sending a steer to the wrong one costs the most.
func TestFleetCarriesTheWorkspacePath(t *testing.T) {
	f := newFleetFixture(t)
	a, b := filepath.Join(shortTempDir(t), "magi"), filepath.Join(shortTempDir(t), "magi")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.daemonAt(a, "one", true)
	f.session("one", a, "first", 1, false)
	f.daemonAt(b, "two", true)
	f.session("two", b, "second", 1, false)

	list := f.get()
	if got := find(t, list, "one").Workdir; got != a {
		t.Errorf("workdir %q, want %q", got, a)
	}
	if got := find(t, list, "two").Workdir; got != b {
		t.Errorf("workdir %q, want %q", got, b)
	}
	// Both are named "magi" — which is the point: the name alone is not enough, and the card shows
	// both because of it.
	if find(t, list, "one").Name != "magi" || find(t, list, "two").Name != "magi" {
		t.Error("the card name should be the base directory")
	}
}

// The viewer's own directory is marked. With several agents in the list, the one you are standing
// in is the one you most often want, and it is otherwise indistinguishable.
func TestFleetMarksTheLocalDaemon(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	mine := f.daemonAt(wd, "mine", true)
	f.session("mine", wd, "local", 1, false)
	f.daemonAt(wd, "theirs", true)
	f.session("theirs", wd, "remote", 1, false)
	f.here = mine

	list := f.get()
	if !find(t, list, "mine").Here {
		t.Error("the local daemon is not marked")
	}
	if find(t, list, "theirs").Here {
		t.Error("another daemon is marked as local")
	}
}

// askingEngine is a daemon blocked on whatever it is told to be blocked on.
type askingEngine struct {
	ask   *app.Ask
	doing string
}

func (e *askingEngine) Doing(session.SessionID) (string, bool) { return e.doing, e.doing != "" }

func (e *askingEngine) Waiting(session.SessionID) (app.Ask, bool) {
	if e.ask == nil {
		return app.Ask{}, false
	}
	return *e.ask, true
}
func (e *askingEngine) Submit(context.Context, command.SubmitPrompt) error { return nil }
func (e *askingEngine) Steer(context.Context, command.SubmitPrompt) error  { return nil }
func (e *askingEngine) Interrupt(context.Context, command.Interrupt) error { return nil }
func (e *askingEngine) RespondPermission(context.Context, command.RespondPermission) error {
	return nil
}
func (e *askingEngine) RespondQuestion(context.Context, command.RespondQuestion) error { return nil }

// serveAsking runs a real daemon behind a published socket, because the fifth state is the one that
// cannot be faked from files: it is asked for over the wire.
func (f *fleetFixture) serveAsking(workdir, sid string, ask *app.Ask) string {
	return f.serveEngine(workdir, sid, &askingEngine{ask: ask})
}

// serveDoing is the same, for a daemon that is not blocked on anybody and is inside a long call.
func (f *fleetFixture) serveDoing(workdir, sid, doing string) string {
	return f.serveEngine(workdir, sid, &askingEngine{doing: doing})
}

func (f *fleetFixture) serveEngine(workdir, sid string, eng daemon.Engine) string {
	f.t.Helper()
	sock := filepath.Join(f.cfgDir, "daemon-"+sid+".sock")
	ctx, cancel := context.WithCancel(context.Background())
	// Cancelled AND waited for: the goroutine writes into a store rooted in a t.TempDir()
	// created earlier, so a cancel that only asks it to stop leaves a write racing the removal.
	// CI reports that as "TempDir RemoveAll cleanup: directory not empty".
	var running sync.WaitGroup
	running.Add(1)
	f.t.Cleanup(func() { cancel(); running.Wait() })
	go func() { defer running.Done(); _ = daemon.Serve(ctx, eng, sock) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	unpublish, err := daemon.Publish(sock, workdir, sid, daemon.Identity{})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(unpublish)
	return sock
}

// The fifth state, and the one the log cannot produce.
//
// A blocked agent has an OPEN TURN in the log, exactly like one running a slow build — so from the
// files alone it reads as "working", which is true and is not the thing that needs doing about it.
// Being blocked has to beat that, and the card has to say what is being decided: "permission: bash"
// asks somebody to approve a category, when the decision is about what it is going to run.
func TestABlockedDaemonIsWaitingAndSaysOnWhat(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.serveAsking(wd, "blocked", &app.Ask{
		ID: "call_7", Kind: "permission", What: "bash",
		Args:   json.RawMessage(`{"command":"rm -rf build"}`),
		Reason: "destructive command detected", Since: time.Now(),
	})
	f.session("blocked", wd, "clean the tree", 4, false) // an open turn: the log says "working"

	a := find(t, f.get(), "blocked")
	if a.State != fleet.Waiting {
		t.Fatalf("a blocked daemon came back as %q — the log's open turn won", a.State)
	}
	if a.AskID != "call_7" || a.AskKind != "permission" {
		t.Errorf("the answer has nowhere to go: id=%q kind=%q", a.AskID, a.AskKind)
	}
	if !strings.Contains(a.Asking, "rm -rf build") {
		t.Errorf("the card does not say what is being decided: %q", a.Asking)
	}
	if !strings.Contains(a.Asking, "destructive command detected") {
		t.Errorf("the policy's reason is missing, so the decision is made on less: %q", a.Asking)
	}
	// What it was doing is still there, as context rather than as the headline.
	if !strings.Contains(a.Task, "clean the tree") {
		t.Errorf("the open turn was lost: %q", a.Task)
	}
}

// A companion busy with handed-over work says what the work is, not who asked for it.
//
// The label goes on its own line above the request, so the first line of the open turn is the
// attribution. Reported as the task it is the one line that is not the work — and the reader who
// most needs it is the refusal that has to say whether to wait or ask somebody else.
func TestACompanionBusyWithHandedOverWorkNamesTheWorkAndNotTheAsker(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.serveAsking(wd, "design", nil)
	f.session("design", wd,
		fleet.DispatchedFrom("master", "mini")+"\n\nrebuild the index from scratch", 2, false)

	a := find(t, f.get(), "design")
	if strings.Contains(a.Task, "asked by master") {
		t.Errorf("the attribution is being reported as the work: %q", a.Task)
	}
	if !strings.Contains(a.Task, "rebuild the index") {
		t.Errorf("the work is not in what it says it is doing: %q", a.Task)
	}
}

// An ordinary request keeps every word of it, including a short opening line.
func TestAnOrdinaryRequestIsReportedFromItsFirstWord(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.serveAsking(wd, "solo", nil)
	f.session("solo", wd, "two things.\n\nfirst, the index", 2, false)

	if a := find(t, f.get(), "solo"); !strings.HasPrefix(a.Task, "two things.") {
		t.Errorf("a request with a short opening paragraph lost it: %q", a.Task)
	}
}

// A daemon that is NOT blocked must not be reported as waiting — the state exists to mean "somebody
// is needed", and a badge that is always on is a badge nobody reads.
func TestAnUnblockedDaemonIsNotWaiting(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.serveAsking(wd, "free", nil)
	f.session("free", wd, "carry on", 2, false)

	a := find(t, f.get(), "free")
	if a.State != fleet.Working {
		t.Errorf("an unblocked daemon with an open turn came back as %q, want working", a.State)
	}
	if a.Asking != "" || a.AskID != "" {
		t.Errorf("it reported a prompt it does not have: %q / %q", a.Asking, a.AskID)
	}
}

// todos records a plan the way the agent's own todowrite does: the whole list, every time.
func (f *fleetFixture) todos(sid string, td []session.Todo) {
	f.t.Helper()
	b, err := json.Marshal(event.TodosChangedData{Todos: td})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.store.Append(context.Background(), session.SessionID(sid),
		event.Event{Type: event.TypeTodosChanged, Data: b, TS: time.Now()}); err != nil {
		f.t.Fatal(err)
	}
}

// countingReader records how often the two whole-log derivations are asked for.
type countingReader struct {
	fleet.Reader
	rebuilds int // transcript reconstructions (the last line)
	reads    int // whole-log scans (is a turn still open)
	plans    int // whole-log scans (the agent's own todo list)
}

func (c *countingReader) PlanOf(ctx context.Context, sid session.SessionID) ([]session.Todo, error) {
	c.plans++
	return c.Reader.PlanOf(ctx, sid)
}

func (c *countingReader) SessionState(ctx context.Context, sid session.SessionID) ([]session.Message, int64, error) {
	c.rebuilds++
	return c.Reader.SessionState(ctx, sid)
}

func (c *countingReader) UnfinishedTurnOf(ctx context.Context, sid session.SessionID) (app.UnfinishedTurn, bool) {
	c.reads++
	return c.Reader.UnfinishedTurnOf(ctx, sid)
}

// A dashboard refresh must not re-derive from a log that has not changed.
//
// BOTH halves of a row come out of the whole log: the last thing an idle agent said needs the
// transcript reconstructed (12ms on a long session), and whether a turn is still open needs every
// event read and decoded. The dashboard refreshes every three seconds for every agent, so for an
// idle one that is the same two answers re-derived forever, while asking whether anything arrived
// costs 2µs. The cache keeps exactly what the log decides, exactly while its sequence is unchanged.
func TestAnIdleAgentIsNotReDerivedEveryRefresh(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "quiet", true)
	f.session("quiet", wd, "the thing it did", 2, true) // finished: idle, so the last line is shown

	r := &countingReader{Reader: f.reader}
	var cache fleet.Cache
	first, err := fleet.ListCached(context.Background(), r, f.cfgDir, "", &cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Task == "" {
		t.Fatalf("the first listing has no last line: %+v", first)
	}
	after, afterReads := r.rebuilds, r.reads
	if after == 0 || afterReads == 0 {
		t.Fatal("the first listing did not derive anything — the test is measuring nothing")
	}

	for i := 0; i < 5; i++ {
		again, lerr := fleet.ListCached(context.Background(), r, f.cfgDir, "", &cache)
		if lerr != nil {
			t.Fatal(lerr)
		}
		if again[0].Task != first[0].Task {
			t.Fatalf("refresh %d changed the line on an idle agent: %q → %q", i, first[0].Task, again[0].Task)
		}
	}
	if r.rebuilds != after {
		t.Errorf("five refreshes of an idle agent rebuilt the transcript %d more times",
			r.rebuilds-after)
	}
	if r.reads != afterReads {
		t.Errorf("five refreshes of an idle agent re-read the whole log %d more times",
			r.reads-afterReads)
	}

	// And when it says something new, the line follows — a cache that never invalidates is worse
	// than none, because the dashboard then reports a state the log denies.
	f.session("quiet", wd, "something new entirely", 0, true)
	moved, err := fleet.ListCached(context.Background(), r, f.cfgDir, "", &cache)
	if err != nil {
		t.Fatal(err)
	}
	if moved[0].Task == first[0].Task {
		t.Errorf("the agent said something new and the card still shows %q", moved[0].Task)
	}
	if r.rebuilds == after || r.reads == afterReads {
		t.Error("nothing was re-derived after the log grew")
	}
}

// A list endpoint answers with a list, even when there is nothing in it.
//
// Go marshals a nil slice as null, and a client that iterates what it gets throws on the first
// supervisor who has nothing to promote yet — which is every supervisor on their first day. Seen
// live before it was covered.
func TestAnEmptyInterventionListIsStillAList(t *testing.T) {
	f := newFleetFixture(t)
	got, err := fleet.Interventions(context.Background(), f.reader, f.cfgDir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("nothing to report came back as nil, which marshals to null")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Errorf("an empty result serialises as %s", b)
	}
}

// A store that cannot answer the cheap question still gets a full row.
//
// NewSince is an optimisation — "has anything arrived since sequence N" — and an optimisation that
// fails must cost speed, not information. Dropping the row would hide exactly the agent whose store
// is in trouble, which is the one a supervisor most needs to see.
type brokenProbe struct{ fleet.Reader }

func (brokenProbe) NewSince(context.Context, session.SessionID, int64) (int64, bool, error) {
	return 0, false, errors.New("the index is unreadable")
}

func TestAnAgentWhoseProbeFailsIsStillListed(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "trouble", true)
	f.session("trouble", wd, "mid-flight", 3, false) // an open turn: the log is what says so

	var cache fleet.Cache
	list, err := fleet.ListCached(context.Background(), brokenProbe{f.reader}, f.cfgDir, "", &cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("a failed probe produced %d rows", len(list))
	}
	if list[0].State != fleet.Working || list[0].Steps != 3 {
		t.Errorf("the row lost what the log says: %+v", list[0])
	}
	if list[0].Task == "" {
		t.Errorf("the row lost what it is doing: %+v", list[0])
	}
	// Twice, because the first call may have populated nothing to fall back on.
	again, err := fleet.ListCached(context.Background(), brokenProbe{f.reader}, f.cfgDir, "", &cache)
	if err != nil || len(again) != 1 || again[0].State != fleet.Working {
		t.Errorf("the second listing came back as %+v (err %v)", again, err)
	}
}

// What a companion handed out, and what came back. Derived from the receivers' transcripts — the
// label is in their logs — so the answer is findable whether or not the asker is still running.
func TestHandoffsReadTheAnswersOffTheReceiversLogs(t *testing.T) {
	f := newFleetFixture(t)
	done, busy := shortTempDir(t), shortTempDir(t)
	f.daemonAt(done, "design", true)
	f.daemonAt(busy, "api", true)

	f.session("design", done, fleet.DispatchedBy("master")+"\n\nspec the empty state", 1, true)
	f.session("api", busy, fleet.DispatchedBy("master")+"\n\nadd the idempotency key", 2, false)

	var cache fleet.Cache
	list, err := fleet.Handoffs(context.Background(), f.reader, f.cfgDir, "master", &cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("two handoffs came back as %d: %+v", len(list), list)
	}
	var finished, running fleet.Handoff
	for _, h := range list {
		if h.State == fleet.Working {
			running = h
		} else {
			finished = h
		}
	}
	// The request survives the round trip as it was written — the label is stripped, not the words.
	if finished.Request != "spec the empty state" {
		t.Errorf("the request came back as %q", finished.Request)
	}
	if finished.From != "master" {
		t.Errorf("who asked came back as %q", finished.From)
	}
	// An idle receiver's last line IS the answer: there is no reply channel.
	if finished.Answer == "" {
		t.Errorf("a finished handoff carries no answer: %+v", finished)
	}
	// A line taken mid-turn is whatever it happened to be saying, which reads as a conclusion and
	// is not one.
	if running.Answer != "" {
		t.Errorf("a running handoff reported an answer: %+v", running)
	}

	// Asking for somebody else's handoffs finds none, rather than everybody's.
	if other, oerr := fleet.Handoffs(context.Background(), f.reader, f.cfgDir, "nobody", &cache); oerr != nil || len(other) != 0 {
		t.Errorf("a stranger's handoffs came back as %+v (%v)", other, oerr)
	}
	// And with nobody named, everything handed around here.
	if all, aerr := fleet.Handoffs(context.Background(), f.reader, f.cfgDir, "", &cache); aerr != nil || len(all) != 2 {
		t.Errorf("the unfiltered view came back as %+v (%v)", all, aerr)
	}
}

// An ordinary prompt is not a handoff, and a prompt that merely mentions the phrase is not either:
// the label opens the message or it is not a label.
func TestOnlyALabelledRequestCountsAsAHandoff(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "api", true)
	// Shaped exactly like a label — comma, blank line and all — but not at the front, which is the
	// only thing that makes one. Anything looser and quoting a handoff in conversation creates one.
	f.session("api", wd, "somebody said \""+fleet.DispatchedBy("master")+
		"\" in passing\n\nwhich is not a request", 1, true)

	var cache fleet.Cache
	list, err := fleet.Handoffs(context.Background(), f.reader, f.cfgDir, "", &cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("a mention became a handoff: %+v", list)
	}
}

// The plan is cached with everything else the log decides — one probe, one answer. It was not, at
// first: the row kept its counts because it re-read the log every poll, which is the cost the whole
// cache exists to avoid and which no test noticed.
func TestAnIdlePlanIsNotReReadEveryRefresh(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "quiet", true)
	f.session("quiet", wd, "the thing", 1, true)
	f.todos("quiet", []session.Todo{{Content: "one", Status: "completed"}, {Content: "two"}})

	r := &countingReader{Reader: f.reader}
	var cache fleet.Cache
	if _, err := fleet.ListCached(context.Background(), r, f.cfgDir, "", &cache); err != nil {
		t.Fatal(err)
	}
	after := r.plans
	if after == 0 {
		t.Fatal("the first listing read no plan — the test is measuring nothing")
	}
	for i := 0; i < 3; i++ {
		list, err := fleet.ListCached(context.Background(), r, f.cfgDir, "", &cache)
		if err != nil {
			t.Fatal(err)
		}
		if list[0].PlanTotal != 2 || list[0].PlanDone != 1 {
			t.Fatalf("refresh %d lost the counts: %d/%d", i, list[0].PlanDone, list[0].PlanTotal)
		}
	}
	if r.plans != after {
		t.Errorf("three refreshes of an idle agent re-read its plan %d more times", r.plans-after)
	}
}

// "working" is not an answer to "is it stuck".
//
// Steps says how much a turn has done, and says nothing at all about a turn that has been inside
// ONE call for ten minutes — which is exactly the shape somebody is looking at when they start
// wondering whether to interrupt. The tool knows (it is polling), it says so on a transient event,
// and that event never leaves the daemon's process. So the listing asks, on the dial it was making
// anyway, and carries the answer.
func TestAWorkingDaemonSaysWhatItIsInsideOf(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.serveDoing(wd, "busy", "check 6, 4m12s elapsed, still running")
	f.session("busy", wd, "wait for the build", 2, false) // an open turn

	a := find(t, f.get(), "busy")
	if a.State != fleet.Working {
		t.Fatalf("state %q, want working", a.State)
	}
	if a.Doing != "check 6, 4m12s elapsed, still running" {
		t.Errorf("the live note did not reach the listing: %q", a.Doing)
	}
}

// And a daemon that is not working does not carry one.
//
// The note is a live fact. On a finished turn the last thing a tool said before it returned is not
// news — dressed up as a status line it would read as "still running" beside an agent that stopped
// an hour ago, which is worse than saying nothing.
func TestAFinishedTurnCarriesNoLiveNote(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.serveDoing(wd, "done", "check 6, 4m12s elapsed, still running")
	f.session("done", wd, "wait for the build", 2, true) // the turn finished

	a := find(t, f.get(), "done")
	if a.State != fleet.Idle {
		t.Fatalf("state %q, want idle", a.State)
	}
	if a.Doing != "" {
		t.Errorf("an idle agent is reported as being inside %q", a.Doing)
	}
}

// A companion on another machine appears, and is not described as anything it was not observed to be.
//
// The dangerous version of this row is the one that falls through to "stopped": nothing here dialled
// anything, so "nobody is listening" is a claim about a machine this process cannot see — and every
// surface that greys out a stopped agent would grey out the whole rest of the cluster.
func TestACompanionOnAnotherMachineIsListedAsBeingElsewhere(t *testing.T) {
	f := newFleetFixture(t)
	f.knowOf(cluster.Member{Host: "buildbox", Socket: "/far/away.sock", Name: "design",
		Role: "screens", Seen: time.Now()})

	got := f.byName("design")
	if got.State != fleet.Remote {
		t.Fatalf("a companion on buildbox reads as %q", got.State)
	}
	if got.Host != "buildbox" {
		t.Errorf("the machine it is on was lost: %q", got.Host)
	}
	if got.Task != "" || got.Steps != 0 || got.Session != "" {
		t.Errorf("a row was filled in with things nothing here could know: %+v", got)
	}
}

// Recently seen means it can be offered work; a while ago means it is shown and not offered.
func TestASightingIsWhatMakesARemoteCompanionCountAsThere(t *testing.T) {
	f := newFleetFixture(t)
	f.knowOf(
		cluster.Member{Host: "buildbox", Socket: "/a.sock", Name: "fresh", Seen: time.Now()},
		cluster.Member{Host: "buildbox", Socket: "/b.sock", Name: "quiet",
			Seen: time.Now().Add(-30 * time.Minute)},
	)
	if !f.byName("fresh").Live {
		t.Error("a companion seen a moment ago is not counted as there")
	}
	if f.byName("quiet").Live {
		t.Error("a companion nobody has seen for half an hour is counted as there")
	}
	if f.byName("quiet").State != fleet.Remote {
		t.Error("a quiet remote companion stopped being listed as remote")
	}
}

// A companion on another machine is offered, and the machine is named on the line.
//
// Both halves. Work crosses now, so leaving it out would hide somebody who can do the job — and
// two hosts can each have a "design", so a bare name is not an address in a cluster.
func TestTheRosterNamesTheMachineACompanionIsOn(t *testing.T) {
	f := newFleetFixture(t)
	f.knowOf(cluster.Member{Host: "buildbox", Socket: "/far/away.sock", Name: "design", Seen: time.Now()})
	lines := fleet.RosterLines(f.get(), "")
	if !strings.Contains(lines, "design") {
		t.Fatalf("the roster hides a companion that work can be handed to:\n%s", lines)
	}
	if !strings.Contains(lines, "buildbox") {
		t.Fatalf("the roster offers a name without saying which machine it is on:\n%s", lines)
	}
}

// One nobody has sighted lately is not offered: it is shown in the listings and is not a candidate.
func TestTheRosterDoesNotOfferACompanionNobodyHasSeen(t *testing.T) {
	f := newFleetFixture(t)
	f.knowOf(cluster.Member{Host: "buildbox", Socket: "/far/away.sock", Name: "ghost",
		Seen: time.Now().Add(-30 * time.Minute)})
	if lines := fleet.RosterLines(f.get(), ""); strings.Contains(lines, "ghost") {
		t.Fatalf("the roster offers a companion nobody has seen for half an hour:\n%s", lines)
	}
}

// The hub is worked out, not read off what somebody declared once.
//
// The declared hub going down used to take the team's addressability with it: every reader went on
// looking for a companion that was not there. Now the team keeps a speaker.
func TestATeamWhoseDeclaredHubIsGoneElectsAnother(t *testing.T) {
	f := newFleetFixture(t)
	f.teamDaemon("lead", "s_lead", "core", true, false) // declared, and not running
	f.teamDaemon("second", "s_second", "core", false, true)

	if f.byName("lead").Hub {
		t.Error("a companion that is not running is still being called the hub")
	}
	if !f.byName("second").Hub {
		t.Fatalf("the team lost its speaker when the declared one went down")
	}
}

// A declaration is a preference, and two of them are two candidates rather than a conflict.
func TestTwoDeclaredHubsStillLeaveExactlyOne(t *testing.T) {
	f := newFleetFixture(t)
	f.teamDaemon("one", "s_one", "core", true, true)
	f.teamDaemon("two", "s_two", "core", true, true)

	hubs := 0
	for _, a := range f.get() {
		if a.Hub {
			hubs++
		}
	}
	if hubs != 1 {
		t.Fatalf("two companions declaring hub produced %d of them", hubs)
	}
}

// A team whose companions have all stopped has no speaker, and none of them goes on claiming it.
func TestATeamThatIsAllStoppedHasNoHub(t *testing.T) {
	f := newFleetFixture(t)
	f.teamDaemon("one", "s_one", "core", true, false)
	f.teamDaemon("two", "s_two", "core", false, false)

	for _, a := range f.get() {
		if a.Hub {
			t.Fatalf("%s answers for a team in which nothing is running", a.Name)
		}
	}
}

// knowOf writes the sightings this machine has been told about, the way an exchange would.
func (f *fleetFixture) knowOf(ms ...cluster.Member) {
	f.t.Helper()
	if _, err := daemon.LearnMembers(f.cfgDir, asAnotherMachine(f.t, ms), time.Now()); err != nil {
		f.t.Fatal(err)
	}
}

// asAnotherMachine signs records the way the machine they describe would.
//
// A record with no signature is dropped on arrival, which is the point of them — so a fixture that
// hands LearnMembers bare structs is testing the drop, not the listing. The key lives in a config
// directory of its own, which is exactly what "another machine" is: a different key, made
// somewhere else, seen here for the first time.
func asAnotherMachine(t *testing.T, ms []cluster.Member) []cluster.Member {
	t.Helper()
	return daemon.Vouch(t.TempDir(), ms)
}

// teamDaemon publishes a companion that belongs to a team, optionally declaring it speaks for it.
func (f *fleetFixture) teamDaemon(name, sid, team string, hub, live bool) {
	f.t.Helper()
	wd := filepath.Join(f.t.TempDir(), name)
	sock := filepath.Join(f.cfgDir, "daemon-"+sid+".sock")
	if live {
		ln, err := net.Listen("unix", sock)
		if err != nil {
			f.t.Fatal(err)
		}
		f.t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
	} else if err := os.WriteFile(sock, nil, 0o600); err != nil {
		f.t.Fatal(err)
	}
	unpublish, err := daemon.Publish(sock, wd, sid, daemon.Identity{Name: name, Team: team, Hub: hub})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(unpublish)
	f.session(sid, wd, "something", 0, true)
}

func (f *fleetFixture) byName(name string) fleet.Agent {
	f.t.Helper()
	for _, a := range f.get() {
		if a.Name == name {
			return a
		}
	}
	f.t.Fatalf("no companion called %q in the listing", name)
	return fleet.Agent{}
}

// A companion this machine can see for itself is listed once, not once for the sight and once for
// the hearsay.
//
// Sockets live in the config directory, so a shared one — two containers with a mount in common,
// two workstations with a network home — puts another machine's companion right here on disk. It is
// then read out of the directory AND heard about round the cluster, and it comes back twice.
//
// Not cosmetic. An address that matches two rows is refused as ambiguous, and both rows answer to
// the same name — so the refusal offers two identical candidates and there is nothing the reader
// can do with it. The companion becomes unaddressable by being seen too well.
func TestACompanionSeenDirectlyAndHeardAboutIsOneCompanion(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.serveAsking(wd, "shared", nil)
	f.session("shared", wd, "building", 1, true)
	// Republished as a companion that calls its machine something else, which is what a container
	// or a second workstation writing into this directory looks like from here.
	overwritePublishedHost(t, sock, wd, "shared", "buildbox", "design")
	// And heard about, the way an exchange with a third machine would deliver it.
	if _, err := daemon.LearnMembers(f.cfgDir, asAnotherMachine(t, []cluster.Member{{
		Host: "buildbox", Socket: sock, Name: "design", Workdir: wd, Seen: time.Now(),
	}}), time.Now()); err != nil {
		t.Fatal(err)
	}

	var named []fleet.Agent
	for _, a := range f.get() {
		if strings.EqualFold(a.Name, "design") {
			named = append(named, a)
		}
	}
	if len(named) != 1 {
		t.Fatalf("one companion came back as %d rows, which is an address nothing can resolve", len(named))
	}
	// And the row kept is the one read directly, not the sighting: it is strictly better evidence.
	if named[0].State == fleet.Remote {
		t.Errorf("the hearsay won over the record on this disk: %+v", named[0])
	}
}

// A path that exists here but belongs to another machine's companion is still two companions.
//
// The complement of the test above, and the reason the socket alone cannot be the identity: two
// machines belonging to one person keep their checkouts in the same places, so the same path means
// different things on each. Folding them together would hide a real companion behind a local one.
func TestTheSamePathOnTwoMachinesIsTwoCompanions(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.serveAsking(wd, "mine", nil)
	f.session("mine", wd, "building", 1, true)
	overwritePublishedHost(t, sock, wd, "mine", "thishost", "here")
	if _, err := daemon.LearnMembers(f.cfgDir, asAnotherMachine(t, []cluster.Member{{
		Host: "otherbox", Socket: sock, Name: "there", Workdir: wd, Seen: time.Now(),
	}}), time.Now()); err != nil {
		t.Fatal(err)
	}
	var here, there bool
	for _, a := range f.get() {
		switch {
		case strings.EqualFold(a.Name, "here"):
			here = true
		case strings.EqualFold(a.Name, "there"):
			there = true
		}
	}
	if !here || !there {
		t.Fatalf("one of them was folded away: here=%v there=%v", here, there)
	}
}

// overwritePublishedHost rewrites the record beside a socket so it names a different machine —
// which is what another container or workstation publishing into a shared directory leaves here.
func overwritePublishedHost(t *testing.T, sock, workdir, sid, host, name string) {
	t.Helper()
	b, err := json.Marshal(daemon.Info{
		Socket: sock, Workdir: workdir, Session: sid, Name: name, Host: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemon.SessionFile(sock), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// How much a companion has waiting is on its line, and does not reorder it.
//
// It cannot be derived from the state beside it: State is read from the session a person attaches
// to, and handed-over work runs in conversations of its own — so a companion busy with three
// people's requests reads as Idle, correctly, and this is the number that says otherwise.
func TestTheRosterSaysHowMuchIsWaitingWithoutRankingByIt(t *testing.T) {
	list := []fleet.Agent{
		{Name: "zulu", Live: true, Role: "screens", Waiting: 0},
		{Name: "alpha", Live: true, Role: "builds", Waiting: 3},
	}
	line := fleet.RosterLines(list, "")
	if !strings.Contains(line, "3 waiting") {
		t.Errorf("the roster does not say what is queued:\n%s", line)
	}
	// Nothing waiting says nothing — a "0 waiting" on every idle companion is a column of noise.
	if strings.Contains(line, "0 waiting") {
		t.Errorf("an empty queue is announced:\n%s", line)
	}
	// And the busiest is not moved. A list in load order is a recommendation; choosing is the
	// model's, and this is one more fact in front of it.
	if strings.Index(line, "alpha") > strings.Index(line, "zulu") {
		t.Errorf("the roster is ordered by load:\n%s", line)
	}
}

// How much is queued reaches a row from the record, here and from a sighting.
//
// Both halves, because an asker choosing between companions reads one list: a number that only
// survived the local hop would make every companion on another machine look free.
func TestARowCarriesHowMuchWorkIsQueued(t *testing.T) {
	f := newFleetFixture(t)
	sock := f.daemonAt(t.TempDir(), "s_busy", true)
	f.session("s_busy", "/w", "hello", 0, true)
	if err := daemon.Announce(sock, 2, true); err != nil {
		t.Fatal(err)
	}
	f.knowOf(cluster.Member{Host: "buildbox", Socket: "/far.sock", Name: "far",
		Waiting: 5, Handling: true, Seen: time.Now()})

	list := f.get()
	if got := find(t, list, "s_busy"); got.Waiting != 2 || !got.Handling {
		t.Errorf("the local row says %d waiting, handling=%v", got.Waiting, got.Handling)
	}
	if got := f.byName("far"); got.Waiting != 5 || !got.Handling {
		t.Errorf("the row for another machine says %d waiting, handling=%v", got.Waiting, got.Handling)
	}
	// And it is not the same fact as what the person's own session is doing: that turn finished,
	// so this companion reads as idle while holding two pieces of somebody else's work.
	if find(t, list, "s_busy").State == fleet.Working {
		t.Error("queued work was mistaken for the attached session being busy")
	}
}

// A companion in the middle of somebody's request is not shown as having nothing to do.
//
// Its state is read from the session a person attaches to, and handed-over work runs in
// conversations of its own — so the state is right, says Idle, and is not the whole truth.
func TestACompanionInTheMiddleOfHandedOverWorkDoesNotReadAsFree(t *testing.T) {
	busy := fleet.Agent{Name: "design", Live: true, State: fleet.Idle, Handling: true}
	if got := fleet.Carrying(busy); !strings.Contains(got, "in hand") {
		t.Errorf("a companion working through a request carries %q", got)
	}
	// Both halves or neither. One piece in hand and nothing behind it means the next request waits
	// one turn; three behind it is a different answer, and one number cannot tell them apart.
	both := fleet.Carrying(fleet.Agent{Live: true, Handling: true, Waiting: 3})
	if !strings.Contains(both, "in hand") || !strings.Contains(both, "3 waiting") {
		t.Errorf("what it is carrying reads as %q", both)
	}
	if got := fleet.Carrying(fleet.Agent{Name: "calm", Live: true}); got != "" {
		t.Errorf("a companion with nothing handed to it carries %q", got)
	}
	line := fleet.RosterLines([]fleet.Agent{busy}, "")
	if !strings.Contains(line, "in hand") {
		t.Errorf("the roster offers it as free:\n%s", line)
	}
}

// A team address goes to whoever in it has the least on them.
//
// The point of starting a second copy of a companion that keeps refusing is that the second one
// takes some of the work. Sending everything to the elected hub made a team of three work like a
// queue of one, because a companion cannot pass handed-over work on — the no-chaining rule stops
// it — so it all piled up behind whoever had been elected.
func TestATeamAddressGoesToWhoeverHasTheLeastOnThem(t *testing.T) {
	// Named so that the answer cannot come out right by alphabet: the free one sorts last, and the
	// hub sorts first among the idle.
	team := []fleet.Agent{
		{Name: "alpha", Team: "design", Hub: true, Live: true, Waiting: 2},
		{Name: "mike", Team: "design", Live: true, Handling: true},
		{Name: "zulu", Team: "design", Live: true},
	}
	got := fleet.Resolve(team, "design")
	if len(got) != 1 {
		t.Fatalf("a team with three live members resolved to %d of them", len(got))
	}
	// zulu has nothing. mike is holding one piece, which is work in progress and not free capacity
	// — a member mid-turn will not start anything else until it is done.
	if got[0].Name != "zulu" {
		t.Errorf("the work went to %q rather than the member with nothing on it", got[0].Name)
	}

	// An idle team behaves exactly as it always did: one address, the elected one, every time —
	// here the hub is the one that sorts LAST, so nothing but the election can produce it.
	idle := []fleet.Agent{
		{Name: "alpha", Team: "design", Live: true},
		{Name: "zulu", Team: "design", Hub: true, Live: true},
	}
	if got := fleet.Resolve(idle, "design"); len(got) != 1 || !got[0].Hub {
		t.Errorf("a team with nothing to do no longer has one stable address: %+v", got)
	}
}

// A member that is not running has nothing on it, and that must not make it the best candidate.
func TestAStoppedTeamMemberIsNotTheLightestOne(t *testing.T) {
	team := []fleet.Agent{
		{Name: "design-a", Team: "design", Hub: true, Live: true, Waiting: 3},
		{Name: "design-b", Team: "design", Live: false},
	}
	got := fleet.Resolve(team, "design")
	if len(got) != 1 || got[0].Name != "design-a" {
		t.Fatalf("work was addressed to a companion that is not running: %+v", got)
	}
	// And a team where everybody stopped comes back whole, so the caller is told there is nobody
	// rather than being handed one of them.
	gone := []fleet.Agent{
		{Name: "design-a", Team: "design", Hub: true},
		{Name: "design-b", Team: "design"},
	}
	if got := fleet.Resolve(gone, "design"); len(got) != 2 {
		t.Errorf("a team that is all stopped resolved to %+v", got)
	}
}

// A refusal that names two companions has to tell them apart.
//
// "design and design — name one of them" asks for a distinction it has just declined to make, and
// the reader has nothing to do with it. Copies of one companion are exactly the case that produces
// it, and starting copies is what somebody does when one keeps refusing work.
func TestARefusalThatListsTwoCompanionsTellsThemApart(t *testing.T) {
	got := fleet.Names([]fleet.Agent{
		{Name: "design", Workdir: "/w/one", Live: true},
		{Name: "design", Workdir: "/w/two", Live: true},
	})
	for _, want := range []string{"/w/one", "/w/two"} {
		if !strings.Contains(got, want) {
			t.Errorf("two companions with one name are listed as %q", got)
		}
	}
	// A name that is already unique is left alone: a workspace path on every entry is noise in the
	// common case, which is two DIFFERENT companions matching one role.
	plain := fleet.Names([]fleet.Agent{
		{Name: "design", Workdir: "/w/one", Live: true},
		{Name: "build", Workdir: "/w/two", Live: true},
	})
	if strings.Contains(plain, "/w/") {
		t.Errorf("names that were already distinct were qualified anyway: %q", plain)
	}
	// A companion on another machine carries it, because the same name on two hosts is two
	// companions and the difference is which machine the work lands on.
	far := fleet.Names([]fleet.Agent{
		{Name: "design", Host: "buildbox", State: fleet.Remote},
		{Name: "design", Workdir: "/w/here", Live: true},
	})
	if !strings.Contains(far, "buildbox/design") {
		t.Errorf("a companion on another machine is not told from one here: %q", far)
	}
}
