package companion_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/adapter/tool/companion"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/cluster"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A team: real daemons behind real sockets, publishing real records, over one store — the same two
// facts everything here derives from (does the socket answer, and what does the log say).
type team struct {
	t      *testing.T
	cfgDir string
	store  *jsonl.Store
	reader *app.App
}

// heard is a daemon that remembers what it was handed.
//
// It takes work the way a real one does — through the protocol, minting a receipt — because that
// is now the only way in. What it records is the composed message, so a test can still assert on
// the exact bytes a companion receives.
type heard struct {
	mu    sync.Mutex
	parts []string
	state daemon.Handover
	given int
}

func (h *heard) Hand(_ context.Context, label, request string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.parts = append(h.parts, companion.Labelled(label, request))
	h.given++
	return fmt.Sprintf("rcpt-%d", h.given), nil
}

func (h *heard) Handed(context.Context, string) (daemon.Handover, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state, nil
}

func (h *heard) Watch(ctx context.Context, _ string, say func(daemon.Handover) error) error {
	var said daemon.Handover
	for {
		h.mu.Lock()
		now := h.state
		h.mu.Unlock()
		if now != said && now != (daemon.Handover{}) {
			said = now
			if say(now) != nil {
				return nil
			}
		}
		if now.Done || now.Over {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (h *heard) says(v daemon.Handover) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state = v
}

func (h *heard) Submit(_ context.Context, c command.SubmitPrompt) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range c.Parts {
		h.parts = append(h.parts, p.Text)
	}
	return nil
}
func (h *heard) got() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.parts...)
}
func (h *heard) Steer(ctx context.Context, c command.SubmitPrompt) error { return h.Submit(ctx, c) }
func (*heard) Interrupt(context.Context, command.Interrupt) error        { return nil }
func (*heard) RespondPermission(context.Context, command.RespondPermission) error {
	return nil
}
func (*heard) RespondQuestion(context.Context, command.RespondQuestion) error { return nil }
func (*heard) Waiting(session.SessionID) (app.Ask, bool)                      { return app.Ask{}, false }
func (*heard) Doing(session.SessionID) (string, bool)                         { return "", false }

func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "magicomp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func newTeam(t *testing.T) *team {
	t.Helper()
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &team{t: t, cfgDir: shortDir(t), store: st,
		reader: app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})}
}

// member starts a daemon that declares who it is, and gives it a session with one finished turn so
// the fleet reads it as idle.
func (tm *team) member(sid, name, role string, eng daemon.Engine) string {
	return tm.memberOf(sid, name, role, daemon.Identity{Name: name, Role: role}, eng)
}

// memberOf is member for a companion that also declares a team, and whether it speaks for it.
func (tm *team) memberOf(sid, name, role string, id daemon.Identity, eng daemon.Engine) string {
	tm.t.Helper()
	wd := shortDir(tm.t)
	sock := tm.cfgDir + "/daemon-" + sid + ".sock"
	ctx, cancel := context.WithCancel(context.Background())
	tm.t.Cleanup(cancel)
	go func() { _ = daemon.Serve(ctx, eng, sock) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cl, err := daemon.Dial(sock); err == nil {
			cl.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	unpublish, err := daemon.Publish(sock, wd, sid, id)
	if err != nil {
		tm.t.Fatal(err)
	}
	tm.t.Cleanup(unpublish)
	tm.write(sid, wd, []event.Event{
		tm.ev(event.TypeSessionCreated, event.SessionCreatedData{Workdir: wd}),
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m1",
			Parts: []session.Part{{Kind: session.PartText, Text: "get set up"}}}),
		tm.ev(event.TypeTurnFinished, event.TurnFinishedData{}),
	})
	return sock
}

// stoppedMemberOf publishes a companion with nothing listening on its socket — what a daemon that
// has been killed leaves behind, and the state a listing reads as stopped.
func (tm *team) stoppedMemberOf(sid string, id daemon.Identity, _ daemon.Engine) string {
	tm.t.Helper()
	wd := shortDir(tm.t)
	sock := tm.cfgDir + "/daemon-" + sid + ".sock"
	// The socket FILE, with nobody behind it — which is what a killed daemon leaves and what a
	// listing reads as stopped. Without it the record is not found at all, and a test asserting
	// that a stopped companion is refused passes because it was never seen.
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		tm.t.Fatal(err)
	}
	unpublish, err := daemon.Publish(sock, wd, sid, id)
	if err != nil {
		tm.t.Fatal(err)
	}
	tm.t.Cleanup(unpublish)
	tm.write(sid, wd, []event.Event{
		tm.ev(event.TypeSessionCreated, event.SessionCreatedData{Workdir: wd}),
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m1",
			Parts: []session.Part{{Kind: session.PartText, Text: "get set up"}}}),
		tm.ev(event.TypeTurnFinished, event.TurnFinishedData{}),
	})
	return sock
}

// workdirOf is the workspace a member published, read back the way anything else would read it.
func (tm *team) workdirOf(sock string) string {
	tm.t.Helper()
	in, err := daemon.Published(sock)
	if err != nil {
		tm.t.Fatal(err)
	}
	return in.Workdir
}

func (tm *team) ev(typ event.Type, d any) event.Event {
	tm.t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		tm.t.Fatal(err)
	}
	return event.Event{Type: typ, Data: b, TS: time.Now()}
}

func (tm *team) write(sid, wd string, evs []event.Event) {
	tm.t.Helper()
	for _, e := range evs {
		if _, err := tm.store.Append(context.Background(), session.SessionID(sid), e); err != nil {
			tm.t.Fatal(err)
		}
	}
}

// ask runs the tool as the master would, from a session of its own.
func (tm *team) ask(self, called, sid, to, request string) session.ToolResult {
	return tm.askAs(companion.Hand{Self: self, Called: called}, sid, to, request)
}

// askAs runs the tool for a companion with a declared place in a team.
func (tm *team) askAs(tool companion.Hand, sid, to, request string) session.ToolResult {
	res, _ := tm.askWatching(tool, sid, to, request)
	return res
}

// askWatching also returns what the tool registered to wait for. The engine's half of this is
// tested in internal/app; what belongs here is whether the tool asks for a watch at all, and with
// which session — get that wrong and the answer is read out of somebody else's transcript.
func (tm *team) askWatching(tool companion.Hand, sid, to, request string) (session.ToolResult, []port.Elsewhere) {
	tm.t.Helper()
	tool.Reader = func() fleet.Reader { return tm.reader }
	tool.ConfigDir = tm.cfgDir
	tool.Cache = &fleet.Cache{}
	if tool.Machine == "" {
		tool.Machine = daemon.Host() // production sets it; without it nobody reads as a neighbour
	}
	if tool.Reach == nil {
		// The same door production uses. reachCompanion dials a socket published here and relays
		// otherwise; a test that skipped it would be testing a path nothing runs.
		tool.Reach = (&crossing{}).reach()
	}
	// Every call carries a form: it is required, and a fixture that omitted it would be testing
	// the refusal instead of the thing under test.
	args, err := json.Marshal(map[string]string{"to": to, "request": request,
		"so_that":   "I can finish the part I am on",
		"answer_as": "- what you found:\n- anything you could not check:"})
	if err != nil {
		tm.t.Fatal(err)
	}
	var watched []port.Elsewhere
	res, err := tool.Execute(context.Background(), args, port.ToolEnv{
		SessionID: session.SessionID(sid),
		Expect:    func(e port.Elsewhere) error { watched = append(watched, e); return nil },
	})
	if err != nil {
		tm.t.Fatal(err)
	}
	return res, watched
}

func text(t *testing.T, r session.ToolResult) string {
	t.Helper()
	var s string
	if json.Unmarshal(r.Content, &s) != nil {
		return string(r.Content)
	}
	return s
}

// Work reaches the companion the address names, in the words it was written in.
func TestHandOffHandsTheWorkOverUntouched(t *testing.T) {
	tm := newTeam(t)
	design := &heard{}
	tm.member("d", "design", "the design system: component specs and visual review", design)
	master := tm.member("m", "master", "coordinating", &heard{})

	const req = "spec the empty state for the fleet table, and name the exact tokens"
	res, watched := tm.askWatching(companion.Hand{Self: master, Called: "master"}, "m", "visual review", req)
	if res.IsError {
		t.Fatalf("the request was refused: %s", text(t, res))
	}
	got := design.got()
	if len(got) != 1 {
		t.Fatalf("the daemon heard %d messages: %v", len(got), got)
	}
	// Equality on the whole message: the label on its own line, the request byte for byte, then
	// the form the answer must take. Every recorded failure of handing work to another agent here
	// began with somebody's words arriving altered, so "contains" is not enough — a prefix nobody
	// asked for is that defect starting.
	want := companion.DispatchedBy("master") + "\n\n" + req +
		"\n\nIn order to: I can finish the part I am on" +
		"\n\nAnswer in this form, filling it in. If a part cannot be done, keep the part and say " +
		"so under it rather than leaving it out — a gap with a name is something the asker can act " +
		"on, and a missing section is not:\n\n- what you found:\n- anything you could not check:"
	if got[0] != want {
		t.Errorf("the message arrived as\n%q\nwant\n%q", got[0], want)
	}
	// The call returns without waiting, and says so — an agent told only "handed over" polls, and
	// polling for something that takes minutes is the blocking call this exists to avoid.
	for _, want := range []string{"Carry on", "comes back here", "Do not wait", "receipt is"} {
		if !strings.Contains(text(t, res), want) {
			t.Errorf("the result does not say the answer comes back on its own (%q): %q", want, text(t, res))
		}
	}
	// And a watch was registered, keyed on the receipt the receiver minted. Not on a session this
	// side guessed at: which session the work landed in is the receiver's business, and asking
	// about one it did not give out is how somebody else's answer gets delivered as this one.
	if len(watched) != 1 {
		t.Fatalf("%d watches registered for one hand-off: %+v", len(watched), watched)
	}
	if watched[0].Who != "design" || watched[0].Request != req {
		t.Errorf("the watch does not name what it is waiting on: %+v", watched[0])
	}
	if in, err := daemon.Published(tm.socketOf("design")); err != nil {
		t.Fatal(err)
	} else if watched[0].Session == in.Session {
		t.Errorf("the watch names their published session (%q) rather than a receipt", in.Session)
	}
}

// Without a way back, nothing is sent.
//
// This is the difference between the tool and the one it replaces. That one handed work over and
// said "they do not report back, read their transcript later", which leaves the asker polling a
// screen it cannot see or losing the work outright. An engine that cannot deliver an answer is an
// engine where handing work over is worse than not.
func TestHandOffSendsNothingWhenTheAnswerCannotComeBack(t *testing.T) {
	tm := newTeam(t)
	design := &heard{}
	tm.member("d", "design", "component specs", design)
	master := tm.member("m", "master", "coordinating", &heard{})

	tool := companion.Hand{Self: master, Called: "master",
		Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir, Cache: &fleet.Cache{}}
	args, _ := json.Marshal(map[string]string{"to": "design", "request": "do the thing",
		"so_that": "I can carry on", "answer_as": "- what you found:"})
	res, err := tool.Execute(context.Background(), args, port.ToolEnv{SessionID: "m"}) // no Expect
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("work was handed over with no way to hear back: %s", text(t, res))
	}
	if got := design.got(); len(got) != 0 {
		t.Errorf("it was sent anyway: %v", got)
	}
}

// The wait is registered AFTER the work is handed over, and that cannot lose an answer.
//
// It used to be the other way round, and the reason was real: a companion quick enough to finish
// in the gap had its turn already closed when the watch started looking, so the watch waited for
// the turn AFTER it. That gap does not exist any more. The receiving daemon takes the position its
// log stands at BEFORE it starts, and mints the receipt from it — so the answer is found by where
// it is in their log rather than by having been watched for since before it happened.
//
// Registered after because it has to be: the receipt does not exist until the far side has taken
// the work. The property that used to depend on the ordering is now tested where it lives, against
// a real daemon — see cmd/magi's TestAnEarlierAnswerIsNotMistakenForThisOne.
func TestTheWaitIsKeyedOnTheReceiptTheReceiverMinted(t *testing.T) {
	tm := newTeam(t)
	design := &recordingOrder{on: func() {}}
	tm.member("d", "design", "component specs", design)
	master := tm.member("m", "master", "coordinating", &heard{})

	_, watched := tm.askWatching(companion.Hand{Self: master, Called: "master"},
		"m", "design", "do the thing")
	if len(watched) != 1 {
		t.Fatalf("%d waits registered", len(watched))
	}
	if watched[0].Session == "" || watched[0].Session == "d" {
		t.Errorf("the wait is keyed on %q — it should be the receipt, not a session it guessed",
			watched[0].Session)
	}
}

// recordingOrder is a daemon that reports the moment work reaches it.
type recordingOrder struct {
	heard
	on func()
}

func (r *recordingOrder) Submit(ctx context.Context, c command.SubmitPrompt) error {
	r.on()
	return r.heard.Submit(ctx, c)
}

// A request is not passed along. The label is already in the transcript, so the rule reads off what
// happened rather than off a counter this process would lose on a restart.
func TestHandOffWillNotChain(t *testing.T) {
	tm := newTeam(t)
	design := &heard{}
	tm.member("d", "design", "component specs", design)
	middle := tm.member("mid", "middle", "passing things on", &heard{})

	// The middle companion's turn was started by somebody else's request.
	tm.write("mid", "", []event.Event{
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m2",
			Parts: []session.Part{
				{Kind: session.PartText, Text: companion.DispatchedBy("master")},
				{Kind: session.PartText, Text: "do the thing"},
			}}),
	})

	res := tm.ask(middle, "middle", "mid", "design", "you do it")
	if !res.IsError {
		t.Fatal("a dispatched turn passed the work along")
	}
	if !strings.Contains(text(t, res), "not passed along") {
		t.Errorf("the refusal does not say why: %q", text(t, res))
	}
	if got := design.got(); len(got) != 0 {
		t.Errorf("it was sent anyway: %v", got)
	}
}

// Two matches is a refusal, and the refusal names them: the cost of guessing is a turn running in
// somebody else's workspace, which nobody can undo by noticing later.
func TestHandOffRefusesAnAmbiguousAddress(t *testing.T) {
	tm := newTeam(t)
	a, b := &heard{}, &heard{}
	tm.member("a", "one", "the design system", a)
	tm.member("b", "two", "design review and copy", b)
	master := tm.member("m", "master", "coordinating", &heard{})

	res := tm.ask(master, "master", "m", "design", "a spec")
	if !res.IsError {
		t.Fatal("an ambiguous address was delivered")
	}
	for _, want := range []string{"one", "two"} {
		if !strings.Contains(text(t, res), want) {
			t.Errorf("the refusal does not name %q: %q", want, text(t, res))
		}
	}
	if len(a.got())+len(b.got()) != 0 {
		t.Error("work was sent despite the ambiguity")
	}

	// Nobody at all: the answer is the roster, because the next thing to do is name one of them.
	res = tm.ask(master, "master", "m", "database", "an index")
	if !res.IsError || !strings.Contains(text(t, res), "one (the design system)") {
		t.Errorf("an unknown address answered %q", text(t, res))
	}
}

// A companion mid-turn is not refused any more — it takes the work and queues it.
//
// It used to be refused here, before anything crossed, and the reasons were real while there was
// one conversation: a request put into a running turn is re-read BY that turn, and "the answer is
// the next turn that finishes" needed no turn to be open. Handed-over work has its own
// conversation now, so neither holds — and bouncing the request off the right companion because
// they happen to be busy puts the retry on the asker.
//
// Whether they are busy is the daemon's answer, not a row's: it is the only thing that knows what
// it is already doing. What is still refused here is a companion with nobody behind its socket,
// which the asker can see for itself and which would waste a crossing.
func TestACompanionMidTurnTakesTheWorkRatherThanRefusingIt(t *testing.T) {
	tm := newTeam(t)
	busy := &heard{}
	tm.member("b", "design", "component specs", busy)
	master := tm.member("m", "master", "coordinating", &heard{})

	// An open turn: a prompt with no finish after it.
	tm.write("b", "", []event.Event{
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m3",
			Parts: []session.Part{{Kind: session.PartText, Text: "rewriting the tokens"}}}),
	})

	res := tm.ask(master, "master", "m", "design", "another spec")
	if res.IsError {
		t.Fatalf("a busy companion was refused rather than asked: %q", text(t, res))
	}
	if got := busy.got(); len(got) != 1 {
		t.Errorf("%d pieces reached them", len(got))
	}
}

// A companion with nobody behind its socket is still refused, and here rather than after a
// crossing that could only fail.
func TestACompanionThatIsNotRunningIsRefusedBeforeAnythingCrosses(t *testing.T) {
	tm := newTeam(t)
	tm.stoppedMemberOf("g", daemon.Identity{Name: "gone", Role: "screens"}, nil)
	master := tm.member("m", "master", "coordinating", &heard{})

	res := tm.ask(master, "master", "m", "gone", "do it")
	if !res.IsError || !strings.Contains(text(t, res), "not running") {
		t.Errorf("a stopped companion answered %q", text(t, res))
	}
}

// Asking yourself is a loop with one node in it.
func TestHandOffRefusesItself(t *testing.T) {
	tm := newTeam(t)
	self := tm.member("s", "solo", "everything", &heard{})
	if res := tm.ask(self, "solo", "s", "solo", "do it"); !res.IsError ||
		!strings.Contains(text(t, res), "that is you") {
		t.Errorf("asking itself answered %q", text(t, res))
	}
}

// The most recent request is the one that governs.
//
// A companion that was dispatched to yesterday and is asked something by its own supervisor today
// is not still under somebody else's instruction — treating an old label as current would leave it
// permanently unable to ask for help, and nobody would connect the refusal to a turn last week.
func TestOnlyTheCurrentRequestDecidesWhetherItMayAsk(t *testing.T) {
	tm := newTeam(t)
	design := &heard{}
	tm.member("d", "design", "component specs", design)
	mine := tm.member("me", "mine", "my own work", &heard{})

	tm.write("me", "", []event.Event{
		// Once dispatched to…
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "old",
			Parts: []session.Part{{Kind: session.PartText,
				Text: companion.DispatchedBy("master") + "\n\nthe old errand"}}}),
		tm.ev(event.TypeTurnFinished, event.TurnFinishedData{}),
		// …and then asked something by the person who owns this workspace.
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "new",
			Parts: []session.Part{{Kind: session.PartText, Text: "now do my thing"}}}),
	})

	if res := tm.ask(mine, "mine", "me", "design", "a spec please"); res.IsError {
		t.Fatalf("an old label still blocks it: %s", text(t, res))
	}
	if got := design.got(); len(got) != 1 {
		t.Errorf("the request did not arrive: %v", got)
	}
}

// An unreadable log refuses rather than assumes.
//
// The no-chaining rule is read off the transcript, so a transcript that cannot be read is a rule
// that cannot be checked. Being wrong the cautious way costs one refusal a person can override by
// asking directly; being wrong the other way is a chain nobody can see.
type blindReader struct{ fleet.Reader }

func (blindReader) SessionState(context.Context, session.SessionID) ([]session.Message, int64, error) {
	return nil, 0, errors.New("the log is unreadable")
}

func TestHandOffRefusesWhenItCannotTellWhetherItWasDispatchedTo(t *testing.T) {
	tm := newTeam(t)
	design := &heard{}
	tm.member("d", "design", "component specs", design)
	self := tm.member("m", "master", "coordinating", &heard{})

	tool := companion.Hand{
		Reader: func() fleet.Reader { return blindReader{tm.reader} }, ConfigDir: tm.cfgDir,
		Self: self, Called: "master", Cache: &fleet.Cache{},
	}
	args, err := json.Marshal(map[string]string{"to": "design", "request": "a spec"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Execute(context.Background(), args, port.ToolEnv{SessionID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("an unreadable log was treated as permission to hand work on")
	}
	if got := design.got(); len(got) != 0 {
		t.Errorf("it was sent anyway: %v", got)
	}
}

// What each companion has learned is in the roster, and that is what makes one of them a
// specialist: whoever is choosing sees the record and picks accordingly, and the picked one learns
// more. No router decides it — the evidence is shown and the caller chooses.
func TestAHubMaySplitWorkAcrossItsOwnTeamAndNoFurther(t *testing.T) {
	tm := newTeam(t)
	member, outsider := &heard{}, &heard{}
	tm.memberOf("fe1", "buttons", "components", daemon.Identity{
		Name: "buttons", Role: "components", Team: "frontend"}, member)
	tm.memberOf("be1", "billing", "invoices", daemon.Identity{
		Name: "billing", Role: "invoices", Team: "backend"}, outsider)
	hub := tm.memberOf("fe0", "frontend-lead", "speaks for the frontend team",
		daemon.Identity{Name: "frontend-lead", Role: "speaks for the frontend team",
			Team: "frontend", Hub: true}, &heard{})

	// The hub's turn was started by somebody else — the case a plain companion may not pass on.
	tm.write("fe0", "", []event.Event{
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m1",
			Parts: []session.Part{{Kind: session.PartText,
				Text: companion.DispatchedBy("master") + "\n\nrebuild the settings screen"}}}),
	})
	as := companion.Hand{Self: hub, Called: "frontend-lead", Team: "frontend", Hub: true}

	if res := tm.askAs(as, "fe0", "buttons", "the toggle component"); res.IsError {
		t.Fatalf("a hub could not hand work to its own team: %s", text(t, res))
	}
	if got := member.got(); len(got) != 1 || !strings.Contains(got[0], "toggle component") {
		t.Errorf("the team member got %v", got)
	}

	// Outside its own team is where a chain of hubs would start, so it stops here.
	res := tm.askAs(as, "fe0", "billing", "and the invoice screen")
	if !res.IsError {
		t.Fatal("a relaying hub reached outside its team")
	}
	if !strings.Contains(text(t, res), "your own team") {
		t.Errorf("the refusal does not say why: %q", text(t, res))
	}
	if got := outsider.got(); len(got) != 0 {
		t.Errorf("it was sent anyway: %v", got)
	}

	// And a plain member of that team still cannot pass anything on: it is not a hub, which is
	// what bounds the depth at two hops without anybody counting.
	tm.write("fe1", "", []event.Event{
		tm.ev(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m2",
			Parts: []session.Part{{Kind: session.PartText,
				Text: companion.DispatchedBy("frontend-lead") + "\n\nthe toggle component"}}}),
	})
	plain := companion.Hand{Self: "/nope", Called: "buttons", Team: "frontend"}
	if res := tm.askAs(plain, "fe1", "billing", "you do it"); !res.IsError {
		t.Error("a team member passed work on")
	}
}

// Addressing a team reaches the companion that answers for it.
func TestATeamNameReachesItsHub(t *testing.T) {
	tm := newTeam(t)
	hub, member := &heard{}, &heard{}
	tm.memberOf("fe0", "frontend-lead", "speaks for the team",
		daemon.Identity{Name: "frontend-lead", Role: "speaks for the team", Team: "frontend", Hub: true}, hub)
	tm.memberOf("fe1", "buttons", "components",
		daemon.Identity{Name: "buttons", Role: "components", Team: "frontend"}, member)
	master := tm.member("m", "master", "coordinating", &heard{})

	if res := tm.ask(master, "master", "m", "frontend", "rebuild the settings screen"); res.IsError {
		t.Fatalf("addressing a team answered %q", text(t, res))
	}
	if len(hub.got()) != 1 {
		t.Errorf("the hub was not the one addressed: hub=%v member=%v", hub.got(), member.got())
	}
	if len(member.got()) != 0 {
		t.Errorf("a team member was addressed directly: %v", member.got())
	}
}

// A team nobody DECLARED a hub for still has one, and it is the member that can do the most.
//
// This used to refuse, and the refusal was written before there was an election. It is the same
// failure the election exists to remove — a group that cannot be addressed because of what somebody
// did or did not type in a config file — and the answer is now the one MongoDB gives: always elect,
// and let a declaration be a preference rather than the only way to have a speaker.
//
// The one that can do the most, because a hub that can do little forwards: the team lead is where
// team-addressed work lands and the only companion allowed to split it up.
func TestATeamWithNoDeclaredHubStillElectsOne(t *testing.T) {
	tm := newTeam(t)
	few, many := &heard{}, &heard{}
	tm.memberOf("x", "one", "bits",
		daemon.Identity{Name: "one", Role: "bits", Team: "loose", Can: 1}, few)
	tm.memberOf("y", "two", "pieces",
		daemon.Identity{Name: "two", Role: "pieces", Team: "loose", Can: 9}, many)
	master := tm.member("m", "master", "coordinating", &heard{})

	if res := tm.ask(master, "master", "m", "loose", "something"); res.IsError {
		t.Fatalf("a team with no declared hub answered %q", text(t, res))
	}
	if len(many.got()) != 1 || len(few.got()) != 0 {
		t.Errorf("the elected speaker was not the one that can do the most: many=%v few=%v",
			many.got(), few.got())
	}
}

// A team whose companions have all stopped is not addressable, and saying so beats picking one.
//
// The election is over who is THERE. Nobody being there is not a tie to be broken — it is the
// answer, and delivering to a dead companion's socket would be work that looks handed over.
func TestATeamThatIsAllStoppedIsNotAddressableAsAGroup(t *testing.T) {
	tm := newTeam(t)
	a, b := &heard{}, &heard{}
	tm.stoppedMemberOf("x", daemon.Identity{Name: "one", Role: "bits", Team: "loose"}, a)
	tm.stoppedMemberOf("y", daemon.Identity{Name: "two", Role: "pieces", Team: "loose"}, b)
	master := tm.member("m", "master", "coordinating", &heard{})

	res := tm.ask(master, "master", "m", "loose", "something")
	if !res.IsError {
		t.Fatal("a team of stopped companions swallowed the work")
	}
	if len(a.got())+len(b.got()) != 0 {
		t.Error("it was sent anyway")
	}
}

// A large team is read by a model on every dispatch, which is where the real cost of "know
// everyone" is — not the directory read. Narrowing turns a page into a paragraph, and says how
// much it left out so a narrow answer is never mistaken for the whole team.

// socketOf finds a published companion's socket by name.
func (tm *team) socketOf(name string) string {
	tm.t.Helper()
	list, err := daemon.List(tm.cfgDir)
	if err != nil {
		tm.t.Fatal(err)
	}
	for _, in := range list {
		if in.Name == name {
			return in.Socket
		}
	}
	tm.t.Fatalf("no companion called %q is published", name)
	return ""
}

// The roster is IN the description, which is the fix for the defect that removed the old tool.
//
// That one named its recipient as free text with no list anywhere and told the model to go and run
// `companions` first — advice, not a mechanism. Asked to "ssh in and do something", a model
// addressed a companion called "ssh", which does not exist. A set the model is expected to look up
// rather than be shown is a set it guesses at.
func TestTheToolShowsWhoThereIsRatherThanTellingTheModelToLook(t *testing.T) {
	list := []fleet.Agent{
		{Name: "design", Role: "the design system", Team: "frontend", Live: true, Socket: "/s/d.sock"},
		{Name: "api", Role: "the billing API", Live: true, Socket: "/s/a.sock"},
		{Name: "me", Role: "coordinating", Live: true, Socket: "/s/m.sock"},
		{Name: "gone", Role: "was here", Live: false, Socket: "/s/g.sock"},
	}
	got := companion.Hand{Roster: func() (string, int) {
		return fleet.RosterLines(list, "/s/m.sock"), len(fleet.Addressable(list, "/s/m.sock"))
	}}.Description()

	for _, want := range []string{"design", "the design system", "[frontend]", "api", "the billing API"} {
		if !strings.Contains(got, want) {
			t.Errorf("the description does not show %q:\n%s", want, got)
		}
	}
	// Itself is left out: a list that offers you yourself is an invitation to a refusal. So is a
	// companion that is not running.
	for _, unwanted := range []string{"me", "gone"} {
		if strings.Contains(got, "\n  "+unwanted) {
			t.Errorf("the description offers %q:\n%s", unwanted, got)
		}
	}
}

// And with nobody there it says so, rather than showing an empty heading that reads as a list
// still loading.
func TestTheToolSaysWhenThereIsNobodyToHandWorkTo(t *testing.T) {
	got := companion.Hand{Roster: func() (string, int) { return fleet.RosterLines(nil, ""), 0 }}.Description()
	if !strings.Contains(got, "nobody else is running") {
		t.Errorf("an empty roster reads as:\n%s", got)
	}
}

// Work handed to another machine crosses instead of dialling, and what crosses is intact.
//
// The label is the part that must be right. A request arriving unattributed is indistinguishable
// from something a person typed, and the no-chaining rule is read off exactly that mark — so it has
// to carry who asked AND that they are not reachable from over there, which is the one thing the
// local label says that is false across a machine.
func TestWorkForAnotherMachineCrossesWithTheRequestIntact(t *testing.T) {
	tm := newTeam(t)
	far := &farSide{}
	there := tm.abroad("design", far)
	master := tm.member("m", "master", "coordinating", &heard{})

	x := &crossing{}
	res, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "rewrite the settings screen")

	if res.IsError {
		t.Fatalf("handing across answered %q", text(t, res))
	}
	hosts, socks := x.asked()
	if len(hosts) == 0 || hosts[0] != "buildbox" || socks[0] != there.Socket {
		t.Errorf("it reached %v %v", hosts, socks)
	}
	labels, requests, _ := far.took()
	// The request byte for byte, then the form after it — the same shape a neighbour receives, so
	// crossing a machine does not change what a companion is asked for.
	if len(requests) != 1 || !strings.HasPrefix(requests[0], "rewrite the settings screen\n\nIn order to: ") {
		t.Errorf("the request was altered on the way: %q", requests)
	}
	if !strings.HasSuffix(requests[0], "- anything you could not check:") {
		t.Errorf("the form did not cross with it: %q", requests)
	}
	if !strings.HasPrefix(labels[0], fleet.DispatchMark) {
		t.Errorf("the label does not carry the mark the receiver reads: %q", labels[0])
	}
	for _, want := range []string{"master", "mini", "cannot reach them"} {
		if !strings.Contains(labels[0], want) {
			t.Errorf("the label does not say %q: %q", want, labels[0])
		}
	}
	if len(watched) != 1 {
		t.Fatalf("%d waits registered for one crossing", len(watched))
	}
	if watched[0].Session != "rcpt-9" {
		t.Errorf("the wait is keyed on %q, not the receipt the far side minted", watched[0].Session)
	}
	if watched[0].Answer == nil {
		t.Fatal("the wait would read a local log for a transcript on another machine")
	}
	// The two halves of being told instead of asking. Without Ready the wait is pushed to and
	// then polls anyway; without Done the connection holding the stream is never released.
	if watched[0].Ready == nil || watched[0].Done == nil {
		t.Error("the wait has no way to be woken, or no way to let go")
	}
	watched[0].Done()
}

// The far side's refusal is what the model reads, in the far side's words — and it does not read as
// a machine that could not be reached.
//
// The two arrive as one error now that the crossing speaks the protocol, and they want opposite
// reactions: a refusal is somebody to ask later, a broken link is a machine to go and fix. Dressing
// a refusal with "it needs magi on its PATH" would send the reader after the wrong thing.
func TestARefusalFromAnotherMachineComesBackAsItWasWritten(t *testing.T) {
	tm := newTeam(t)
	tm.abroad("design", &farSide{refuse: "design is mid-turn (rebuilding the index)"})
	master := tm.member("m", "master", "coordinating", &heard{})

	x := &crossing{}
	res, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "something")
	if !res.IsError {
		t.Fatal("a refusal from the far side read as success")
	}
	said := text(t, res)
	if !strings.Contains(said, "mid-turn") {
		t.Errorf("the far side's reason was lost: %q", said)
	}
	if strings.Contains(said, "PATH") || strings.Contains(said, "ssh") {
		t.Errorf("a companion that answered was reported as a machine that could not be reached: %q", said)
	}
	if len(watched) != 0 {
		t.Error("a wait was registered for work that was refused")
	}
}

// The answer is fetched from the machine that has it, and only once it is finished.
func TestTheAnswerIsFetchedFromTheMachineThatHasIt(t *testing.T) {
	tm := newTeam(t)
	far := &farSide{}
	tm.abroad("design", far)
	master := tm.member("m", "master", "coordinating", &heard{})

	x := &crossing{}
	_, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "something")
	if len(watched) != 1 {
		t.Fatalf("%d waits registered", len(watched))
	}
	defer watched[0].Done()
	if _, done := watched[0].Answer(); done {
		t.Fatal("an unfinished turn on another machine reported an answer")
	}
	far.says(daemon.Handover{Done: true, Answer: "the screen is rewritten"})

	// Pushed, not polled. The wait is woken by the far side rather than by its own clock — which
	// is the whole change: that clock is three seconds because it was written for reading a log
	// file on this disk, and across a machine every tick of it was a process.
	nudged(t, watched[0])
	got, done := watched[0].Answer()
	if !done || got != "the screen is rewritten" {
		t.Fatalf("the finished answer came back as (%q, %v)", got, done)
	}
}

// A companion that restarted with the work unfinished ends the wait rather than leaving it silent.
//
// A running daemon cannot report this about itself, so it arrives as the one answer a restarted one
// can give: it does not know the receipt. That is only reachable because it ANSWERED — a link that
// is merely down looks entirely different and must not end anything.
func TestACompanionThatRestartedMidWorkEndsTheWait(t *testing.T) {
	tm := newTeam(t)
	far := &farSide{}
	tm.abroad("design", far)
	master := tm.member("m", "master", "coordinating", &heard{})

	x := &crossing{}
	_, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "something")
	if len(watched) != 1 {
		t.Fatalf("%d waits registered", len(watched))
	}
	defer watched[0].Done()
	if news, over := watched[0].Probe(); over {
		t.Fatalf("the wait ended before anything went wrong: %q", news)
	}
	far.forgets()
	nudged(t, watched[0])
	news, over := watched[0].Probe()
	if !over {
		t.Fatalf("a companion that forgot the work left the wait running: %q", news)
	}
	if !strings.Contains(news, "design on buildbox") {
		t.Errorf("the news does not say who or where: %q", news)
	}
}

// A magi with no way to cross says so rather than dialling a path that means something else here.
func TestWithNoWayAcrossItRefusesInsteadOfDiallingLocally(t *testing.T) {
	tm := newTeam(t)
	tm.elsewhere(cluster.Member{Host: "buildbox", Socket: "/far/d.sock", Name: "design", Seen: time.Now()})
	master := tm.member("m", "master", "coordinating", &heard{})

	res, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini"}, "m", "design", "something")
	if !res.IsError {
		t.Fatal("work was handed somewhere with no way to reach it")
	}
	if len(watched) != 0 {
		t.Error("a wait was registered for work that never left")
	}
}

// A machine that does not answer is not a machine that lost the work.
//
// An ssh fails for a dropped connection far more often than for a companion that has died, and a
// probe that read a failed call as "nothing will come back" would end the wait on a bad wifi hop.
func TestAMachineThatDoesNotAnswerDoesNotEndTheWait(t *testing.T) {
	tm := newTeam(t)
	far := &farSide{}
	tm.abroad("design", far)
	master := tm.member("m", "master", "coordinating", &heard{})

	x := &crossing{}
	_, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "something")
	if labels, _, _ := far.took(); len(labels) != 1 || len(watched) != 1 {
		t.Fatalf("labels=%v waits=%d", labels, len(watched))
	}
	defer watched[0].Done()
	// The stream is up; now take the machine away under it. The reconnect that follows must not
	// be read as news, because a link drops for a closed laptop lid far more often than for a
	// companion that has died.
	settles(t, "the stream never reached the far side", func() bool {
		_, _, shown := far.took()
		return len(shown) > 0
	})
	x.breaks(errors.New("ssh: connect to host buildbox port 22: Network is unreachable"))
	time.Sleep(100 * time.Millisecond)
	if news, over := watched[0].Probe(); over || news != "" {
		t.Errorf("an unreachable machine ended the wait: %q over=%v", news, over)
	}
	if _, done := watched[0].Answer(); done {
		t.Error("an unreachable machine produced a finished answer")
	}
}

// elsewhere records a sighting of a companion on another machine, the way an exchange would.
func (tm *team) elsewhere(ms ...cluster.Member) {
	tm.t.Helper()
	if _, err := daemon.LearnMembers(tm.cfgDir, ms, time.Now()); err != nil {
		tm.t.Fatal(err)
	}
}

// farSide is a companion "on another machine": a real daemon, behind a real socket, answering the
// real protocol. Only the pipe is different — a dial instead of an ssh — so what these tests
// exercise is the wire, the client and the receipt rather than a mock of them.
type farSide struct {
	heard
	fmu      sync.Mutex
	refuse   string
	state    daemon.Handover
	forgot   bool // answers as a daemon that has restarted: it does not know the receipt
	labels   []string
	requests []string
	shown    []string // the receipts presented to it on the way back
}

func (f *farSide) Hand(_ context.Context, label, request string) (string, error) {
	f.fmu.Lock()
	defer f.fmu.Unlock()
	if f.refuse != "" {
		return "", errors.New(f.refuse)
	}
	f.labels, f.requests = append(f.labels, label), append(f.requests, request)
	return "rcpt-9", nil
}

func (f *farSide) Handed(_ context.Context, receipt string) (daemon.Handover, error) {
	f.fmu.Lock()
	defer f.fmu.Unlock()
	f.shown = append(f.shown, receipt)
	if f.forgot {
		return daemon.Handover{}, errors.New("no handover here with that receipt")
	}
	return f.state, nil
}

// Watch is the fake's half of the stream: it says the current state, then says it again whenever
// the test changes it, and ends when the state is an ending. The 10ms loop stands in for a bus.
func (f *farSide) Watch(ctx context.Context, receipt string, say func(daemon.Handover) error) error {
	f.fmu.Lock()
	f.shown = append(f.shown, receipt)
	f.fmu.Unlock()

	var said daemon.Handover
	for {
		f.fmu.Lock()
		forgot, now := f.forgot, f.state
		f.fmu.Unlock()
		if forgot {
			return errors.New("no handover here with that receipt")
		}
		if now != said && now != (daemon.Handover{}) {
			said = now
			if say(now) != nil {
				return nil
			}
		}
		if now.Done || now.Over {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (f *farSide) took() (labels, requests, shown []string) {
	f.fmu.Lock()
	defer f.fmu.Unlock()
	return append([]string(nil), f.labels...), append([]string(nil), f.requests...),
		append([]string(nil), f.shown...)
}

func (f *farSide) says(h daemon.Handover) {
	f.fmu.Lock()
	defer f.fmu.Unlock()
	f.state = h
}

// forgets is what a restart looks like from outside: the same companion, the same log, no memory
// of having taken anything.
func (f *farSide) forgets() {
	f.fmu.Lock()
	defer f.fmu.Unlock()
	f.forgot = true
}

// nudged waits for the far side to push something.
//
// Generous, because a push has a connection to set up and, where a link was cut, a backoff to sit
// through first. A test that read the state on the next line would be testing its own timing.
func nudged(t *testing.T, e port.Elsewhere) {
	t.Helper()
	select {
	case <-e.Ready:
	case <-time.After(20 * time.Second):
		t.Fatal("nothing was pushed: the wait would have had to poll for it")
	}
}

// settles waits for a condition the far side pushes towards, for the cases where the nudge itself
// is not the thing under test.
func settles(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(what)
}

// abroad starts a companion whose socket is NOT in this machine's config directory — so it is
// reachable only by being sighted, which is what makes it remote — and records the sighting.
func (tm *team) abroad(name string, eng daemon.Engine) cluster.Member {
	tm.t.Helper()
	sock := shortDir(tm.t) + "/d.sock"
	ctx, cancel := context.WithCancel(context.Background())
	tm.t.Cleanup(cancel)
	go func() { _ = daemon.Serve(ctx, eng, sock) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cl, err := daemon.Dial(sock); err == nil {
			cl.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m := cluster.Member{Host: "buildbox", Socket: sock, Name: name, Role: "screens", Seen: time.Now()}
	tm.elsewhere(m)
	return m
}

// crossing is a Reach that dials instead of spawning ssh, and remembers what it was asked to reach.
type crossing struct {
	mu     sync.Mutex
	hosts  []string
	socks  []string
	broken error
	// goneAfter makes every crossing past the nth reach the machine and find no companion. A
	// count rather than a flag because the handing over is itself a crossing, and the thing under
	// test is what happens to the one after it.
	goneAfter int
}

// cut is a crossing whose stream closes with nothing said — what a daemon dying mid-watch leaves
// behind, and what a daemon that has finished leaves behind, which is the whole problem.
type cut struct{ n int32 }

func (c *cut) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *cut) Write(b []byte) (int, error) { atomic.AddInt32(&c.n, 1); return len(b), nil }
func (c *cut) Close() error                { return nil }

func (c *crossing) reach() companion.Reach {
	return func(_ context.Context, host, socket string) (*daemon.Client, error) {
		c.mu.Lock()
		c.hosts, c.socks = append(c.hosts, host), append(c.socks, socket)
		broken, n, gone := c.broken, len(c.hosts), c.goneAfter
		c.mu.Unlock()
		if broken != nil {
			return nil, broken
		}
		if gone > 0 && n > gone {
			return daemon.Over(noCompanion{}), nil
		}
		return daemon.Dial(socket)
	}
}

// noCompanion is a crossing that got to the far machine and found nothing behind the socket — what
// the relay over there reports by its exit code, and the one failure a caller on this side could
// never work out for itself.
type noCompanion struct{}

func (noCompanion) Read([]byte) (int, error) {
	return 0, fmt.Errorf("%w: nothing is listening at that socket", daemon.ErrGone)
}
func (noCompanion) Write(b []byte) (int, error) { return len(b), nil }
func (noCompanion) Close() error                { return nil }

func (c *crossing) asked() (hosts, socks []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.hosts...), append([]string(nil), c.socks...)
}

func (c *crossing) breaks(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.broken = err
}

// The roster names what each companion can do, so a model can choose without asking anybody.
func TestTheRosterLineNamesWhatACompanionCanDo(t *testing.T) {
	tm := newTeam(t)
	tm.elsewhere(cluster.Member{Host: "buildbox", Socket: "/far/d.sock", Name: "design",
		Role: "screens", Can: 5, Does: []string{"tokens", "layout", "contrast"}, Seen: time.Now()})
	tm.member("m", "master", "coordinating", &heard{})

	list, err := fleet.List(context.Background(), tm.reader, tm.cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	line := fleet.RosterLines(list, "")
	for _, want := range []string{"tokens", "layout", "contrast"} {
		if !strings.Contains(line, want) {
			t.Errorf("the roster does not say the companion can %q:\n%s", want, line)
		}
	}
	// Five things, three named: the line must not imply the list is the whole answer.
	if !strings.Contains(line, "(+2)") {
		t.Errorf("the roster hides that there are more:\n%s", line)
	}
}

// Asking what somebody can do reaches the machine that has them, and answers in their words.
func TestAskingWhatACompanionCanDoReachesTheMachineThatHasThem(t *testing.T) {
	tm := newTeam(t)
	tm.elsewhere(cluster.Member{Host: "buildbox", Socket: "/far/d.sock", Name: "design",
		Can: 1, Does: []string{"tokens"}, Seen: time.Now()})

	var askedHost, askedSocket string
	tool := companion.About{
		Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir, Cache: &fleet.Cache{},
		Ask: func(_ context.Context, host, socket string) (string, error) {
			askedHost, askedSocket = host, socket
			return "design — screens\n\nWhat it has written procedures for:\n  tokens — names the colour roles\n", nil
		},
	}
	args, _ := json.Marshal(map[string]string{"who": "design"})
	res, err := tool.Execute(context.Background(), args, port.ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("asking answered %q", text(t, res))
	}
	// An address and nothing else. A name would have to be resolved on the far side, against a
	// config directory that login may not be able to read — which is what this stopped doing.
	if askedHost != "buildbox" || askedSocket != "/far/d.sock" {
		t.Errorf("it asked %q at %q", askedHost, askedSocket)
	}
	if !strings.Contains(text(t, res), "names the colour roles") {
		t.Errorf("the description did not come back: %q", text(t, res))
	}
}

// A machine that cannot be asked says which one, rather than answering nothing.
func TestAMachineThatCannotDescribeSaysWhichOne(t *testing.T) {
	tm := newTeam(t)
	tm.elsewhere(cluster.Member{Host: "buildbox", Socket: "/far/d.sock", Name: "design", Seen: time.Now()})
	tool := companion.About{
		Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir, Cache: &fleet.Cache{},
		Ask: func(context.Context, string, string) (string, error) {
			return "", errors.New("Network is unreachable")
		},
	}
	args, _ := json.Marshal(map[string]string{"who": "design"})
	res, _ := tool.Execute(context.Background(), args, port.ToolEnv{})
	if !res.IsError {
		t.Fatal("an unreachable machine read as a description")
	}
	if !strings.Contains(text(t, res), "buildbox") {
		t.Errorf("the failure does not name the machine: %q", text(t, res))
	}
}

// A machine that answers and has no companion ends the wait, where a machine that does not answer
// does not.
//
// The two are the same missing bytes from here and opposite instructions: one is nothing coming
// ever, the other is a link to try again. Only the far side can tell them apart, so this is really
// a test that its answer is carried rather than flattened into "the crossing failed".
func TestAMachineThatAnswersWithNoCompanionEndsTheWait(t *testing.T) {
	tm := newTeam(t)
	tm.abroad("design", &farSide{})
	master := tm.member("m", "master", "coordinating", &heard{})

	x := &crossing{goneAfter: 1} // the handing over is real; everything after finds nobody
	_, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "something")
	if len(watched) != 1 {
		t.Fatalf("%d waits registered", len(watched))
	}
	defer watched[0].Done()
	nudged(t, watched[0])
	news, over := watched[0].Probe()
	if !over {
		t.Fatalf("a machine that said the companion is gone left the wait running: %q", news)
	}
	if !strings.Contains(news, "design on buildbox") || !strings.Contains(news, "no longer running") {
		t.Errorf("the news does not say who, where, or what happened: %q", news)
	}
}

// A stream that was cut is not a stream that finished.
//
// A daemon that dies with a watch open closes the socket, and from the asking side that is byte for
// byte what a daemon looks like when it has said its last word and hung up. Read as finished, the
// watch stops and nobody is ever told anything — the wait sits for its full two hours. Observed by
// killing a companion mid-work across two containers.
//
// The last word is the only thing that separates them, so a close without one has to reconnect, and
// the reconnect is what establishes which happened.
func TestAStreamThatWasCutIsNotAStreamThatFinished(t *testing.T) {
	tm := newTeam(t)
	tm.abroad("design", &farSide{})
	master := tm.member("m", "master", "coordinating", &heard{})

	// The handing over is real; the watch after it is cut with nothing said; everything after that
	// reaches the machine and finds no companion, which is what a dead daemon answers.
	x := &cutting{goneAfter: 2}
	_, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "something")
	if len(watched) != 1 {
		t.Fatalf("%d waits registered", len(watched))
	}
	defer watched[0].Done()
	nudged(t, watched[0])
	news, over := watched[0].Probe()
	if !over {
		t.Fatalf("a cut stream was read as a finished one, so nothing was ever said: %q", news)
	}
	if !strings.Contains(news, "no longer running") {
		t.Errorf("the news does not say what happened: %q", news)
	}
}

// cutting reaches for real once, then hands back a stream that closes saying nothing, then reports
// a machine with no companion on it.
type cutting struct {
	mu        sync.Mutex
	n         int
	goneAfter int
}

func (c *cutting) reach() companion.Reach {
	return func(_ context.Context, _, socket string) (*daemon.Client, error) {
		c.mu.Lock()
		c.n++
		n := c.n
		c.mu.Unlock()
		switch {
		case n == 1:
			return daemon.Dial(socket)
		case n <= c.goneAfter:
			return daemon.Over(&cut{}), nil
		}
		return daemon.Over(noCompanion{}), nil
	}
}

// Work handed across is asked about by its receipt and by nothing else.
//
// The position it stands for stays on the machine that has it, so this side can name the work it
// handed over and has no way to name anybody else's — not by design of a check, but because it
// holds nothing that would let it. This side never learns the far session at all: the wait is keyed
// on the receipt, which is the only handle that ever crossed.
func TestTheAnswerIsAskedForByReceiptAndNothingElse(t *testing.T) {
	tm := newTeam(t)
	far := &farSide{state: daemon.Handover{Done: true, Answer: "ok"}}
	tm.abroad("design", far)
	master := tm.member("m", "master", "coordinating", &heard{})

	x := &crossing{}
	_, watched := tm.askWatching(
		companion.Hand{Self: master, Called: "master", Machine: "mini", Reach: x.reach()},
		"m", "design", "something")
	if len(watched) != 1 {
		t.Fatalf("%d waits registered", len(watched))
	}
	defer watched[0].Done()
	nudged(t, watched[0])
	watched[0].Answer()

	_, _, shown := far.took()
	if len(shown) != 1 || shown[0] != "rcpt-9" {
		t.Errorf("the question presented %v, not the receipt the far side minted", shown)
	}
	if watched[0].Session != "rcpt-9" {
		t.Errorf("this side is holding %q, which is something other than the receipt it was given",
			watched[0].Session)
	}
}

// A companion that comes up after this one does is advertised.
//
// The list used to be taken once, at startup, and a daemon that came up before its cluster had
// converged advertised nobody for the life of the process. Asked to hand work over, the model
// answered that no such companion exists — it had never been shown one. The refusal path reads the
// live list, so this was supposed to cost a turn; but a model shown nobody does not guess, it
// declines, and the turn is never spent. Observed across five machines.
func TestACompanionThatAppearsLaterIsAdvertised(t *testing.T) {
	tm := newTeam(t)
	master := tm.member("m", "master", "coordinating", &heard{})

	// Built the way production builds it: a function over the live listing, not a string.
	tool := companion.Hand{Self: master, Called: "master", Roster: func() (string, int) {
		list, err := fleet.List(context.Background(), tm.reader, tm.cfgDir, master)
		if err != nil {
			return "", 0
		}
		return fleet.RosterLines(list, master), len(fleet.Addressable(list, master))
	}}
	if got := tool.Description(); strings.Contains(got, "design") {
		t.Fatalf("a companion that does not exist yet is already offered:\n%s", got)
	}

	tm.member("d", "design", "the design system", &heard{})
	if got := tool.Description(); !strings.Contains(got, "design") {
		t.Errorf("a companion that came up later is not offered, so nothing can address it:\n%s", got)
	}
}

// dialing is the door as production opens it for a companion on this machine.
func dialing() companion.Reach { return (&crossing{}).reach() }
