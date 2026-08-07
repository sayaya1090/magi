package fleet_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
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
type askingEngine struct{ ask *app.Ask }

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
	f.t.Helper()
	sock := filepath.Join(f.cfgDir, "daemon-"+sid+".sock")
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	go func() { _ = daemon.Serve(ctx, &askingEngine{ask: ask}, sock) }()
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

// countingReader records how often the two whole-log derivations are asked for.
type countingReader struct {
	fleet.Reader
	rebuilds int // transcript reconstructions (the last line)
	reads    int // whole-log scans (is a turn still open)
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
