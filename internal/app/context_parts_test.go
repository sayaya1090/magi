package app

import (
	"context"
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
