package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// An interactive top-level ask_user blocks on question.requested and resumes
// with the user's pick; the answer lands in the tool result the model sees.
func TestAskUserRoundTrip(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("ask_user", `{"questions":[{"question":"which approach?","options":["redis","in-memory"]}]}`),
		textStep("done"),
	}}
	store, _ := jsonl.New(t.TempDir())
	reg := builtin.Default()
	reg.Register(builtin.AskUser{})
	a := New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Interactive: true})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	wd := t.TempDir()
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "pick"}}})

	answered := false
	var toolOut string
	for e := range ch {
		switch e.Type {
		case event.TypeQuestionRequested:
			var d event.QuestionRequestedData
			_ = json.Unmarshal(e.Data, &d)
			if d.Question != "which approach?" || len(d.Options) != 2 {
				t.Fatalf("question event wrong: %+v", d)
			}
			answered = true
			a.RespondQuestion(context.Background(), command.RespondQuestion{SessionID: sid, CallID: d.CallID, Answer: "in-memory"})
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
				var s string
				_ = json.Unmarshal(d.Part.ToolResult.Content, &s)
				toolOut = s
			}
		}
		if e.Type == event.TypeTurnFinished {
			goto done
		}
	}
done:
	if !answered {
		t.Fatal("question.requested never arrived")
	}
	if !strings.Contains(toolOut, "A: in-memory") {
		t.Fatalf("tool result should carry the pick, got %q", toolOut)
	}
}

// Headless (non-interactive) never blocks: the tool errors instructively.
func TestAskUserHeadlessDegrades(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("ask_user", `{"questions":[{"question":"q?","options":["a","b"]}]}`),
		textStep("done"),
	}}
	store, _ := jsonl.New(t.TempDir())
	reg := builtin.Default()
	reg.Register(builtin.AskUser{})
	a := New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Interactive: false})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "pick"}}})
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("headless ask_user must not hang the turn: %v", typesOf(got))
	}
}
