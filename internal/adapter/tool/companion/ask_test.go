package companion_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/adapter/tool/companion"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
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
type heard struct {
	mu    sync.Mutex
	parts []string
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
	return tm.askAs(companion.Ask{Self: self, Called: called}, sid, to, request)
}

// askAs runs the tool for a companion with a declared place in a team.
func (tm *team) askAs(tool companion.Ask, sid, to, request string) session.ToolResult {
	tm.t.Helper()
	tool.Reader = func() fleet.Reader { return tm.reader }
	tool.ConfigDir = tm.cfgDir
	tool.Cache = &fleet.Cache{}
	args, err := json.Marshal(map[string]string{"to": to, "request": request})
	if err != nil {
		tm.t.Fatal(err)
	}
	res, err := tool.Execute(context.Background(), args, port.ToolEnv{SessionID: session.SessionID(sid)})
	if err != nil {
		tm.t.Fatal(err)
	}
	return res
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
func TestAskHandsTheWorkOverUntouched(t *testing.T) {
	tm := newTeam(t)
	design := &heard{}
	tm.member("d", "design", "the design system: component specs and visual review", design)
	master := tm.member("m", "master", "coordinating", &heard{})

	const req = "spec the empty state for the fleet table, and name the exact tokens"
	res := tm.ask(master, "master", "m", "visual review", req)
	if res.IsError {
		t.Fatalf("the request was refused: %s", text(t, res))
	}
	got := design.got()
	if len(got) != 1 {
		t.Fatalf("the daemon heard %d messages: %v", len(got), got)
	}
	// Equality on the whole message: the label on its own line, then the request byte for byte.
	// Every recorded failure of handing work to another agent here began with somebody's words
	// arriving altered, so "contains" is not enough — a prefix nobody asked for is that defect
	// starting.
	if want := companion.DispatchedBy("master") + "\n\n" + req; got[0] != want {
		t.Errorf("the message arrived as\n%q\nwant\n%q", got[0], want)
	}
	// And the answer tells the caller where to read the reply, since nothing reports back.
	if !strings.Contains(text(t, res), "companions") {
		t.Errorf("the result does not say how to get the answer: %q", text(t, res))
	}
}

// A request is not passed along. The label is already in the transcript, so the rule reads off what
// happened rather than off a counter this process would lose on a restart.
func TestAskWillNotChain(t *testing.T) {
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
func TestAskRefusesAnAmbiguousAddress(t *testing.T) {
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

// A companion mid-turn is not handed anything: a prompt sent to a running turn is re-read BY that
// turn, so it would land inside the work they are already doing rather than after it.
func TestAskWillNotLandInsideSomebodyElsesTurn(t *testing.T) {
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
	if !res.IsError {
		t.Fatal("work was pushed into a running turn")
	}
	if !strings.Contains(text(t, res), "mid-turn") || !strings.Contains(text(t, res), "rewriting the tokens") {
		t.Errorf("the refusal does not say what they are busy with: %q", text(t, res))
	}
	if got := busy.got(); len(got) != 0 {
		t.Errorf("it was sent anyway: %v", got)
	}
}

// Asking yourself is a loop with one node in it.
func TestAskRefusesItself(t *testing.T) {
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

func TestAskRefusesWhenItCannotTellWhetherItWasDispatchedTo(t *testing.T) {
	tm := newTeam(t)
	design := &heard{}
	tm.member("d", "design", "component specs", design)
	self := tm.member("m", "master", "coordinating", &heard{})

	tool := companion.Ask{
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
func TestTheRosterSaysWhatEachHasLearned(t *testing.T) {
	tm := newTeam(t)
	design := tm.member("d", "design", "component specs", &heard{})
	tm.member("api", "api", "billing", &heard{})
	self := tm.member("m", "master", "coordinating", &heard{})

	// The design companion has a record; the api one does not.
	dwd := tm.workdirOf(design)
	if err := expgit.New(dwd+"/.magi/experience").Propose(context.Background(), port.Contribution{
		Skills: []port.Skill{
			{Name: "tokens", Description: "spacing tokens come from the scale, never hand-written"},
			{Name: "empty-states", Description: "an empty state says what would be here and why"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	tool := companion.List{
		Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir,
		Self: self, Cache: &fleet.Cache{},
	}
	res, err := tool.Execute(context.Background(), nil, port.ToolEnv{SessionID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	out := text(t, res)
	if !strings.Contains(out, "spacing tokens come from the scale") {
		t.Errorf("the roster does not say what design has learned:\n%s", out)
	}
	// And it does not invent a record for the one with none: an empty line would read as "this one
	// has been asked and learned nothing", which is a different and untrue thing. Sliced to the one
	// block, because the whole listing obviously contains the phrase — for the other companion.
	block := out[strings.Index(out, "api  "):strings.Index(out, "design  ")]
	if strings.Contains(block, "has learned") {
		t.Errorf("a companion with no record claims one:\n%s", block)
	}
}

// A hub splitting a piece of work across its own team is the reason to have a hub, so that one
// hand-on is allowed — and only that one.
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
	as := companion.Ask{Self: hub, Called: "frontend-lead", Team: "frontend", Hub: true}

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
	plain := companion.Ask{Self: "/nope", Called: "buttons", Team: "frontend"}
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

// A team nobody speaks for is not addressable as a group, and saying so beats picking a member.
func TestATeamWithNoHubIsNotAddressableAsAGroup(t *testing.T) {
	tm := newTeam(t)
	a, b := &heard{}, &heard{}
	tm.memberOf("x", "one", "bits", daemon.Identity{Name: "one", Role: "bits", Team: "loose"}, a)
	tm.memberOf("y", "two", "pieces", daemon.Identity{Name: "two", Role: "pieces", Team: "loose"}, b)
	master := tm.member("m", "master", "coordinating", &heard{})

	res := tm.ask(master, "master", "m", "loose", "something")
	if !res.IsError {
		t.Fatal("a team with no hub swallowed the work")
	}
	for _, want := range []string{"one", "two"} {
		if !strings.Contains(text(t, res), want) {
			t.Errorf("the refusal does not name %q: %q", want, text(t, res))
		}
	}
	if len(a.got())+len(b.got()) != 0 {
		t.Error("it was sent anyway")
	}
}

// A large team is read by a model on every dispatch, which is where the real cost of "know
// everyone" is — not the directory read. Narrowing turns a page into a paragraph, and says how
// much it left out so a narrow answer is never mistaken for the whole team.
func TestTheRosterCanBeNarrowedToTheWorkAtHand(t *testing.T) {
	tm := newTeam(t)
	tm.member("d", "design", "component specs and visual review", &heard{})
	tm.member("a", "api", "billing and invoices", &heard{})
	tm.member("o", "ops", "deploys and alerting", &heard{})
	self := tm.member("m", "master", "coordinating", &heard{})

	list := companion.List{
		Reader: func() fleet.Reader { return tm.reader }, ConfigDir: tm.cfgDir,
		Self: self, Cache: &fleet.Cache{},
	}
	run := func(args string) string {
		t.Helper()
		res, err := list.Execute(context.Background(), json.RawMessage(args), port.ToolEnv{SessionID: "m"})
		if err != nil {
			t.Fatal(err)
		}
		return text(t, res)
	}

	narrowed := run(`{"matching":"visual review of the settings screen"}`)
	if !strings.Contains(narrowed, "design") {
		t.Errorf("the one who could do it is missing:\n%s", narrowed)
	}
	for _, gone := range []string{"billing and invoices", "deploys and alerting"} {
		if strings.Contains(narrowed, gone) {
			t.Errorf("%q survived the filter:\n%s", gone, narrowed)
		}
	}
	// Always said. A narrowed list that does not admit it is narrowed reads as the whole team, and
	// the reader concludes nobody else could have done the work.
	if !strings.Contains(narrowed, "did not match") {
		t.Errorf("the filter is silent about what it hid:\n%s", narrowed)
	}
	// The caller is always in its own answer: one that asked "who does design" and cannot see
	// itself may hand its own work away.
	if !strings.Contains(narrowed, "this is you") {
		t.Errorf("the caller filtered itself out:\n%s", narrowed)
	}

	// Short words match everything, which would make a filter into a no-op that looks like a
	// filter — the worst of both, since the caller believes it narrowed.
	if wide := run(`{"matching":"do the a of it"}`); !strings.Contains(wide, "Nobody else matched") {
		t.Errorf("a query of only short words matched somebody:\n%s", wide)
	}

	// Nobody matching is not nobody being there.
	none := run(`{"matching":"kubernetes"}`)
	if !strings.Contains(none, "Nobody else matched") || !strings.Contains(none, "did not match") {
		t.Errorf("an empty result reads as an empty machine:\n%s", none)
	}

	// And no filter is still the whole team.
	if all := run(`{}`); !strings.Contains(all, "billing") || !strings.Contains(all, "deploys") {
		t.Errorf("the unfiltered roster is short:\n%s", all)
	}

	// A `matching` of the wrong type is reported, not shrugged off into "no filter" — that would
	// hand back the whole team as if it had been asked for.
	if bad := run(`{"matching":3}`); !strings.Contains(bad, "is a string") {
		t.Errorf("a mistyped filter silently widened the answer:\n%s", bad)
	}
}
