package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

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

	// The turn runs on its own goroutine; the make-up lands on its finish.
	var st ContextState
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err = a.ContextStateOf(context.Background(), sid)
		if err != nil {
			t.Fatal(err)
		}
		if st.Parts.Sum() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
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
	st, err := a.ContextStateOf(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts != (ContextParts{}) {
		t.Errorf("the reading invented a make-up for a companion that has never run: %+v", st.Parts)
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
	a.notePromptShape("s", sys, msgs, []port.ToolSpec{{Name: "bash", Description: "run a command"}})
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

// The two shapes are the same shape.
//
// ContextParts converts from event.PromptShape. A field added to the fact and not to the reading
// (or the reverse) has to stop the build, because the silent version of that mistake is a piece of
// the context that is recorded and then never shown.
func TestTheRecordedShapeAndTheReportedPartsMatch(t *testing.T) {
	p := ContextParts(event.PromptShape{System: 1, Tools: 2, Talk: 3, Calls: 4, Results: 5})
	if p.Sum() != 15 {
		t.Errorf("the parts sum to %d, so the conversion dropped a field", p.Sum())
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
	if st.Parts != (ContextParts{}) {
		t.Errorf("the reading still carries the pre-fold make-up %+v — drawn under a post-fold "+
			"total, that band renders the fold as if it had not happened", st.Parts)
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
	var st ContextState
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, err = a.ContextStateOf(context.Background(), sid); err == nil && st.Parts.Sum() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
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
