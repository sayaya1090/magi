package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The context reading says what the window is filled WITH, and the two biggest pieces are ones a
// reader of the log could never measure.
//
// The system prompt and the tool catalog are assembled per session and never written down. Before
// this, the console's context panel counted the conversation and called that the context — so a
// companion holding a 7k tool catalog and nothing else reported "~0 tokens", and a person watching
// a full window was pointed at the conversation, which on this harness is routinely the small half.
func TestTheContextReadingSaysWhatTheWindowIsFilledWith(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("done")}}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: reg})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: dir, Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid,
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"},
		Parts: []session.Part{{Kind: session.PartText, Text: "say something"}}}); err != nil {
		t.Fatal(err)
	}

	// The turn runs on its own goroutine; the RECORDED make-up lands on its finish. (A reading
	// before the finish already carries an assembled make-up, so "parts > 0" is not the signal —
	// the finish is.)
	waitForFinish(t, a, sid)
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts.System == 0 {
		t.Error("the reading reports no system prompt, which no request has ever had — the panel " +
			"is describing the conversation and calling it the context")
	}
	if st.Parts.Tools == 0 {
		t.Error("the tool catalog rides on every request and is the largest single piece on the " +
			"default roster, and the reading says it is not there")
	}
	if st.Parts.Talk == 0 {
		t.Error("a turn was run with a prompt and the reading counted no conversation")
	}
	if sum := st.Parts.Sum(); sum < st.Parts.System+st.Parts.Tools {
		t.Errorf("the parts sum to %d, less than the two pieces it just reported", sum)
	}
	// And the window this process knew is on the fact — measured through a READER over the same
	// log, which is the only way to see it. This App knows the model, so it fills the window in
	// from its own registry whether or not the recording side wrote anything down; a console does
	// not, and that is the half that breaks silently.
	reader := New(a.store, nil, builtin.NewRegistry(), bus.New(), nil, Config{})
	rst, rerr := reader.ContextStateOf(context.Background(), sid)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if rst.Window != 8000 {
		t.Errorf("a reader over this log gets a window of %d where the session's model is 8000 — "+
			"the recording side did not put it on the fact, and a console has no other way to it",
			rst.Window)
	}
}

// A session that has never assembled a request records no make-up rather than a zeroed one.
//
// Zero is not a measurement here: every real request carries a system prompt and a tool catalog,
// so five zeros written as a fact would say "this companion is holding nothing", which is a thing
// that never happens. Absent lets the screen leave the graph undrawn, the same rule it already
// follows for a context window it does not know.
func TestASessionThatNeverRanRecordsNoPromptShape(t *testing.T) {
	llm := &fakeLLM{}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: reg})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: dir, Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := shapeOf(a, sid); got != nil {
		t.Errorf("a session that assembled no request reports a make-up of %+v", *got)
	}
	// The READING is a different thing from the record: it says what the first request will be
	// made of, assembled now from the same pieces — so the bar is never empty where the system
	// prompt and the catalog belong (TestTheReadingCarriesSystemAndToolsBeforeTheFirstTurn).
	// What it must not do is count a conversation that has not happened.
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts.Talk != 0 || st.Parts.Calls != 0 || st.Parts.Results != 0 {
		t.Errorf("the reading invented a conversation for a companion that has never run: %+v", st.Parts)
	}
}

// Each kind is measured over its own characters, because the screen prints each number under its
// own colour and a person reads them one at a time.
//
// The five will not sum to estimateTokens — five roundings against one — and this pins each kind
// separately rather than the sum, which is the thing that has to be true. The lengths here are
// deliberately not multiples of four: an earlier version of this test used round numbers and let a
// mutation that counted a kind the wrong way pass, because the arithmetic happened to agree.
func TestEachKindIsMeasuredOverItsOwnCharacters(t *testing.T) {
	sys := "seven-and-a-bit chars of system prompt, not a multiple of four!!"
	talk := "abcdefghijk"                     // 11
	args := []byte(`{"command":"ls -la /x"}`) // 23
	res := []byte(`"a file listing, longer"`) // 24
	msgs := []session.Message{{Role: session.RoleUser, Parts: []session.Part{
		{Kind: session.PartText, Text: talk},
		{ToolCall: &session.ToolCall{Name: "bash", Args: args}},
		{ToolResult: &session.ToolResult{Content: res}},
	}}}
	a := &App{}
	a.notePromptShape("s", "", sys, msgs, []port.ToolSpec{{Name: "bash", Description: "run a command"}})
	sh, ok := a.promptShape("s")
	if !ok {
		t.Fatal("nothing was recorded")
	}
	for _, c := range []struct {
		kind      string
		got, want int
	}{
		{"system", sh.System, len(sys) / 4},
		{"talk", sh.Talk, len(talk) / 4},
		{"calls", sh.Calls, (len("bash") + len(args)) / 4},
		{"results", sh.Results, len(res) / 4},
		{"tools", sh.Tools, (len("bash") + len("run a command")) / 4},
	} {
		if c.got != c.want {
			t.Errorf("%s counted as %d, want %d", c.kind, c.got, c.want)
		}
	}
}

// The five parts come across, and the window does not join them.
//
// partsOf takes the recorded fact apart. The window rides on the same fact — a reader cannot work
// it out for itself — but it is what the parts are measured AGAINST, and folding it into the sum
// would put the whole context window inside the bar that shows how full the context window is.
func TestTheRecordedShapeAndTheReportedPartsMatch(t *testing.T) {
	p := partsOf(event.PromptShape{Window: 100000, System: 1, Tools: 2, Talk: 3, Calls: 4, Results: 5})
	if p.Sum() != 15 {
		t.Errorf("the parts sum to %d — either a field was dropped, or the window joined them", p.Sum())
	}
}

// The terminal and the console answer the context question from one derivation.
//
// They did not. ContextView worked the numbers out itself and passed an EMPTY system prompt to the
// estimator, so `/context` reported the conversation and called it the context — the same defect
// the console had, surviving in the surface nobody looked at when the console was fixed. Two
// derivations of one fact is the arrangement where exactly one of them gets repaired.
func TestTheTerminalAndTheConsoleReadOneContext(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("done")}}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: reg})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: dir, Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid,
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"},
		Parts: []session.Part{{Kind: session.PartText, Text: "say something"}}}); err != nil {
		t.Fatal(err)
	}
	var st ContextState
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, err = a.ContextStateOf(context.Background(), sid); err == nil && st.Parts.Sum() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Parts.Sum() == 0 {
		t.Fatal("nothing was recorded for the turn")
	}

	view, err := a.ContextView(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	// The same total, spelled the way the terminal spells it — and that total is the whole
	// request, not the transcript. Both halves matter: the estimate used to be the conversation
	// alone, so a terminal that "agreed" with the console agreed on a number that was wrong in
	// both places.
	if want := commas(st.Used); !strings.Contains(view, want) {
		t.Errorf("the terminal reports a different total from the console (%s): %s", want, view)
	}
	// The number has to be at least the two pieces the transcript cannot supply. Comparing it
	// against the conversation instead passes on an estimate that counts talk, calls and results
	// and still leaves out the prompt and the catalog — measured: that version of this assertion
	// let exactly that mutation through.
	if floor := st.Parts.System + st.Parts.Tools; st.Used < floor {
		t.Errorf("the reading is %d, below the %d of system prompt and tool catalog it is sitting "+
			"on — the estimate is still the transcript alone", st.Used, floor)
	}
	// And it says what that total is made of, naming the two pieces the log cannot supply.
	for _, want := range []string{commas(st.Parts.System), commas(st.Parts.Tools), "system", "tools"} {
		if !strings.Contains(view, want) {
			t.Errorf("the terminal's reading does not mention %q — a person reading it there still "+
				"cannot see that most of the window is the prompt and the catalog: %s", want, view)
		}
	}
}

// A fold clears the make-up, the way it clears the total.
//
// A compaction is the one moment the conversation actually shrinks, so the breakdown from before
// it describes a context that no longer exists — and the piece it is most wrong about is the one
// this reading exists to show. The total already had this rule; the make-up was added beside it
// and did not get it, so a folded session drew the pre-fold band under a post-fold number.
func TestAFoldClearsTheMakeUpWithTheTotal(t *testing.T) {
	a, dir := newApp(t, &fakeLLM{}, Config{Permission: "allow", Models: model.NewRegistry()})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ev := func(typ event.Type, d any) event.Event {
		b, merr := json.Marshal(d)
		if merr != nil {
			t.Fatal(merr)
		}
		return event.Event{Type: typ, Data: b, TS: time.Now()}
	}
	// A finished turn that recorded what it was made of, and then a fold.
	if _, err := a.store.Append(context.Background(), sid,
		ev(event.TypeTurnFinished, event.TurnFinishedData{
			Usage:  event.Usage{In: 9000},
			Prompt: &event.PromptShape{System: 2404, Tools: 5703, Talk: 800, Results: 93},
		}),
		ev(event.TypeCompaction, event.CompactionData{TokensBefore: 9000, TokensAfter: 1200}),
	); err != nil {
		t.Fatal(err)
	}

	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	// The pre-fold make-up is gone. What stands in its place is assembled now — the system prompt
	// and the catalog the next request will carry, over the folded transcript (empty here) — so the
	// band under the post-fold total describes the post-fold context, not the one that no longer
	// exists.
	if pre := partsOf(event.PromptShape{System: 2404, Tools: 5703, Talk: 800, Results: 93}); st.Parts == pre {
		t.Errorf("the reading still carries the pre-fold make-up %+v — drawn under a post-fold "+
			"total, that band renders the fold as if it had not happened", st.Parts)
	}
	if st.Parts.Talk != 0 || st.Parts.Calls != 0 || st.Parts.Results != 0 {
		t.Errorf("after the fold the reading counts a conversation the folded log does not hold: %+v", st.Parts)
	}
	if st.Used > 9000 {
		t.Errorf("the total survived the fold too: %d", st.Used)
	}
}

// The live meter counts the tool catalog, like every other reading of this number.
//
// context.usage is what the JetBrains plugin reads, and it is the ONLY thing it reads about the
// window — so when the console and the terminal were corrected, the IDE gauge was left as the last
// surface running 6-7k tokens light on the default roster. The compaction trigger has counted the
// catalog since the day it was measured; this is the same sum, in the meter people watch.
func TestTheLiveMeterCountsTheToolCatalog(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("done")}}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: reg})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: dir, Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var meters []int
	watchCtx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	ch, unsub := a.bus.Subscribe(watchCtx, sid)
	defer unsub()
	go func() {
		for e := range ch {
			if e.Type != event.TypeContextUsage {
				continue
			}
			var d event.ContextUsageData
			if json.Unmarshal(e.Data, &d) == nil {
				mu.Lock()
				meters = append(meters, d.Tokens)
				mu.Unlock()
			}
		}
	}()

	if err := a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid,
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"},
		Parts: []session.Part{{Kind: session.PartText, Text: "say something"}}}); err != nil {
		t.Fatal(err)
	}
	waitForFinish(t, a, sid)
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts.Tools == 0 {
		t.Fatal("no tool catalog was recorded, so this test cannot measure what it is for")
	}
	mu.Lock()
	got := append([]int(nil), meters...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("the live meter never fired")
	}
	// Every reading has to clear the catalog on its own: a meter that counts the conversation and
	// the prompt is still short by the largest single piece of the request.
	for i, n := range got {
		if n < st.Parts.Tools {
			t.Errorf("meter %d reported %d tokens, below the %d of tool catalog every request "+
				"carries — this is the number the IDE gauge draws", i, n, st.Parts.Tools)
		}
	}
}

// A reader that cannot know the context window is told it.
//
// The window comes from a model registry and a backend probe. A console builds its own App over
// the log with an empty registry and no prober, so its own answer is 0 for every companion — and
// 0 is what the screens read as "unknown", which is the state where they deliberately draw no
// gauge at all. Measured against a daemon that had probed the same model and knew 262,144: the
// panel showed a token count with nothing to measure it against.
func TestTheWindowTravelsOnTheRecordedFact(t *testing.T) {
	// A reader: no models, no prober — exactly what the web console constructs.
	reader, dir := newApp(t, &fakeLLM{}, Config{Permission: "allow", Models: model.NewRegistry()})
	sid, err := reader.CreateSession(context.Background(), command.CreateSession{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.contextWindow("some-model"); got != 0 {
		t.Fatalf("this reader claims to know a window (%d); the test cannot measure what it is for", got)
	}

	b, err := json.Marshal(event.TurnFinishedData{
		Prompt: &event.PromptShape{Window: 262144, System: 2404, Tools: 5703, Talk: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.store.Append(context.Background(), sid,
		event.Event{Type: event.TypeTurnFinished, Data: b, TS: time.Now()}); err != nil {
		t.Fatal(err)
	}

	st, err := reader.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Window != 262144 {
		t.Errorf("the reading reports a window of %d — the screen then has a token count and "+
			"nothing to measure it against, and draws no gauge", st.Window)
	}
	// And the window is not one of the parts: it is what they are measured against.
	if st.Parts.Sum() >= 262144 {
		t.Errorf("the window was folded into the parts (sum %d) — the bar would show the whole "+
			"window sitting inside the window", st.Parts.Sum())
	}
}

// Before the first turn the bar was empty where the two biggest pieces belong. The system prompt
// and the tool catalog exist the moment the session does — this process assembles them for every
// request — so a reading before any request says what the first one WILL carry, and a reader over
// the log, which assembles nothing, keeps saying nothing.
func TestTheReadingCarriesSystemAndToolsBeforeTheFirstTurn(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("done")}}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: reg})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: dir, Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts.System == 0 || st.Parts.Tools == 0 {
		t.Fatalf("before any turn the reading says system=%d tools=%d — the pieces every request carries, counted as absent", st.Parts.System, st.Parts.Tools)
	}
	if st.Parts.Talk != 0 || st.Used != st.Parts.Sum() || !st.Estimated {
		t.Errorf("an empty conversation should read talk=0 and an estimated total equal to the parts: %+v used=%d estimated=%v", st.Parts, st.Used, st.Estimated)
	}
	// Measuring froze nothing: the head is written by the first request, not by a reading.
	a.mu.Lock()
	frozen := a.stateLocked(sid).skillBlockSet || a.stateLocked(sid).turnSysSet || len(a.stateLocked(sid).toolsFrozen) > 0
	a.mu.Unlock()
	if frozen {
		t.Error("a reading pinned the session's head — the skill block, the system prompt or the catalog was frozen by looking")
	}
	reader := New(a.store, nil, builtin.NewRegistry(), bus.New(), nil, Config{})
	rst, err := reader.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if rst.Parts.System != 0 || rst.Parts.Tools != 0 {
		t.Errorf("a reader over the log assembles nothing and should not invent a make-up: %+v", rst.Parts)
	}
}

// The band and the number beside it are drawn from one ruler: the count the backend reported. The
// five pieces are arithmetic; when a real count exists they keep their proportions and take its size.
func TestThePartsTakeTheProviderCountsSize(t *testing.T) {
	a, dir := newApp(t, &fakeLLM{}, Config{Permission: "allow", Models: model.NewRegistry()})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(event.TurnFinishedData{
		Usage:  event.Usage{In: 10000},
		Prompt: &event.PromptShape{System: 2404, Tools: 5703, Talk: 800, Calls: 300, Results: 900}, // sums to 10107
	})
	if _, err := a.store.Append(context.Background(), sid, event.Event{Type: event.TypeTurnFinished, Data: b, TS: time.Now()}); err != nil {
		t.Fatal(err)
	}
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Used != 10000 || st.Estimated {
		t.Fatalf("the total should be the provider's count: used=%d estimated=%v", st.Used, st.Estimated)
	}
	if st.Parts.Sum() != 10000 {
		t.Errorf("the pieces sum to %d under a total of 10000 — two rulers on one band: %+v", st.Parts.Sum(), st.Parts)
	}
	if st.Parts.Tools < st.Parts.System || st.Parts.System < st.Parts.Results || st.Parts.Results < st.Parts.Talk || st.Parts.Talk < st.Parts.Calls {
		t.Errorf("scaling changed the proportions: %+v", st.Parts)
	}
	if got := (ContextParts{}).scaledTo(100); got != (ContextParts{}) {
		t.Errorf("an empty make-up has no proportions to keep, got %+v", got)
	}
}

// The reading counts what the window holds AFTER the turn: the answer is in it. A companion that
// has answered cannot read "talk 1" — the person who saw that number had just read a paragraph
// from it (2026-09-06).
func TestTheReadingCountsTheAnswerThatCameBack(t *testing.T) {
	long := strings.Repeat("답 ", 400) // ~800 chars of answer, ~200 tokens
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep(long)}}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: reg})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: dir, Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid,
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"},
		Parts: []session.Part{{Kind: session.PartText, Text: "하나만"}}}); err != nil {
		t.Fatal(err)
	}
	waitForFinish(t, a, sid)
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts.Talk < 100 {
		t.Errorf("the companion answered ~200 tokens and the reading says the conversation is %d — it measured the request that was sent, not the context that came back: %+v", st.Parts.Talk, st.Parts)
	}
}

// The reading takes what the window HELD at the end of the turn, not the turn's bill. The bill sums
// In over every step, so a six-step turn billed 221k against a 35k context — and the make-up,
// scaled to the bill, drew a 196k tool catalog (Excel, 2026-09-07).
func TestTheReadingTakesWhatTheWindowHeldNotTheBill(t *testing.T) {
	a, dir := newApp(t, &fakeLLM{}, Config{Permission: "allow", Models: model.NewRegistry()})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(event.TurnFinishedData{
		Usage:  event.Usage{In: 221482, Out: 3000, Cached: 167852, CacheReported: true}, // six steps, summed
		Held:   &event.Usage{In: 35000, Out: 500, Cached: 30000, CacheReported: true},   // the last one
		Prompt: &event.PromptShape{System: 3000, Tools: 31000, Talk: 400},
	})
	if _, err := a.store.Append(context.Background(), sid, event.Event{Type: event.TypeTurnFinished, Data: b, TS: time.Now()}); err != nil {
		t.Fatal(err)
	}
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Used != 35500 || st.Cached != 30000 {
		t.Fatalf("the reading took the bill for the window: used=%d cached=%d", st.Used, st.Cached)
	}
	if st.Parts.Tools > 35500 {
		t.Errorf("the tool catalog was scaled to the bill: %+v", st.Parts)
	}
}

// A finished turn records what the window held — the last step's own count — beside the bill.
func TestAFinishRecordsWhatTheWindowHeld(t *testing.T) {
	llm := &usageLLM{text: "done", in: 1234, out: 56}
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: model.NewRegistry()})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	evs := runToTerminal(t, a, sid)
	var d event.TurnFinishedData
	found := false
	for _, e := range evs {
		if e.Type == event.TypeTurnFinished && json.Unmarshal(e.Data, &d) == nil {
			found = true
		}
	}
	if !found {
		t.Fatal("no turn.finished")
	}
	if d.Held == nil || d.Held.In != 1234 || d.Held.Out != 56 {
		t.Fatalf("the finish did not record what the window held: %+v", d.Held)
	}
}
