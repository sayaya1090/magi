package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// endingTool is a plugin-shaped tool that ends its turn (what the landing plugin's land does
// through magi.finish).
type endingTool struct{ a *App }

func (endingTool) Name() string            { return "landish" }
func (endingTool) Description() string     { return "declares the turn done" }
func (endingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t endingTool) Execute(_ context.Context, _ json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	t.a.PluginFinish(string(env.SessionID))
	return session.ToolResult{Content: json.RawMessage(`"landed — the turn may end here"`)}, nil
}

// A tool that ends the turn ends it in the same step when the answer is already written: no
// further model call, one turn.finished. Live (Excel, 2026-09-07) the model re-called land seven
// times because nothing but its own next call-less step could end the turn.
func TestAToolCanEndTheTurnItRunsIn(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		append([]port.ProviderEvent{{Type: port.ProviderText, Text: "시트 목록: Sheet1 (1개)"}}, toolStep("landish", `{}`)...),
		textStep("never asked for"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 40})
	a.tools.Register(endingTool{a: a})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "list"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("want one finish: %v", typesOf(got))
	}
	llm.mu.Lock()
	calls := llm.call
	llm.mu.Unlock()
	if calls != 1 {
		t.Errorf("the model was asked again after the tool ended the turn: %d calls", calls)
	}
}

// With no answer written yet, the mark lapses and the next step ends the turn carrying the answer.
func TestAToolEndingBeforeAnyAnswerLetsTheNextStepAnswer(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("landish", `{}`),
		textStep("여기 답입니다"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow", MaxSteps: 40})
	a.tools.Register(endingTool{a: a})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "list"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("want one finish: %v", typesOf(got))
	}
	llm.mu.Lock()
	calls := llm.call
	llm.mu.Unlock()
	if calls != 2 {
		t.Errorf("with no answer yet the next step should have been asked: %d calls", calls)
	}
}
