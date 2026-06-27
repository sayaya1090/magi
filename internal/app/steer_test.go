package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Steer on an idle session behaves like Submit: it starts a turn that finishes.
func TestSteerIdleStartsRun(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("ok")}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	if err := a.Steer(context.Background(), command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		t.Fatal(err)
	}
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) != 1 {
		t.Fatalf("steer-idle: expected 1 turn.finished, got %v", typesOf(got))
	}
}

// hasUnansweredUserPrompt is true right after a user prompt, false after the
// assistant has responded.
func TestHasUnansweredUserPrompt(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("answer")}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	// Append a user prompt without running a turn → unanswered.
	_ = a.appendPrompt(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "q"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	if !a.hasUnansweredUserPrompt(ctx, sid) {
		t.Fatal("expected unanswered user prompt before any run")
	}

	// Run a turn → the assistant answers → no longer unanswered.
	a.startRun(ctx, sid)
	waitForTerminal(t, a, sid)
	if a.hasUnansweredUserPrompt(ctx, sid) {
		t.Fatal("expected no unanswered prompt after the assistant responded")
	}
}

// A message steered in while the agent is busy in a tool call is picked up in the
// same turn: the next model request includes it. A blocking tool holds the turn
// open so the test can inject deterministically.
func TestSteerMidTurnPickedUp(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	bt := &blockingTool{started: started, release: release}

	reg := builtin.Default()
	reg.Register(bt)

	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec := &recordingLLM{
		steps: [][]port.ProviderEvent{
			{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_block", Name: "block", Args: json.RawMessage(`{}`)}}, {Type: port.ProviderFinish}},
			textStep("done"),
		},
	}
	a := New(store, rec, reg, bus.New(), nil, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "start"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	<-started // the blocking tool is now executing (turn is mid-flight)
	if err := a.Steer(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "ALSO_DO_THIS"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		t.Fatal(err)
	}
	close(release) // let the tool finish; the loop reads events again for step 2

	waitForTerminal(t, a, sid)

	// The second model request must contain the steered message.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.reqs) < 2 {
		t.Fatalf("expected >=2 model requests, got %d", len(rec.reqs))
	}
	if !requestContains(rec.reqs[len(rec.reqs)-1], "ALSO_DO_THIS") {
		t.Fatal("steered message was not present in the continued turn's context")
	}
}

func requestContains(r port.ChatRequest, s string) bool {
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			if p.Kind == session.PartText && strings.Contains(p.Text, s) {
				return true
			}
		}
	}
	return false
}

// recordingLLM is a scripted provider that records each request it receives.
type recordingLLM struct {
	mu    sync.Mutex
	steps [][]port.ProviderEvent
	reqs  []port.ChatRequest
	call  int
}

func (f *recordingLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	evs := textStep("done")
	if f.call < len(f.steps) {
		evs = f.steps[f.call]
	}
	f.call++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 8)
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// blockingTool blocks until released, holding a turn open mid-flight.
type blockingTool struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingTool) Name() string            { return "block" }
func (b *blockingTool) Description() string     { return "test tool that blocks until released" }
func (b *blockingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (b *blockingTool) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return session.ToolResult{Content: mustJSON("blocked then released")}, nil
}

func mustJSON(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
