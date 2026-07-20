package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
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
	if !a.hasUnansweredUserPrompt(ctx, sid, nil) {
		t.Fatal("expected unanswered user prompt before any run")
	}

	// Run a turn → the assistant answers → no longer unanswered.
	a.startRun(ctx, sid)
	waitForTerminal(t, a, sid)
	if a.hasUnansweredUserPrompt(ctx, sid, nil) {
		t.Fatal("expected no unanswered prompt after the assistant responded")
	}
}

// A message steered in while the agent is busy is treated as an interjection: it
// is deferred (masked from the in-flight turn's model context so it cannot merge
// into the current task) and then drained to run as its OWN subsequent turn. This
// asserts both halves of that contract — the running turn does NOT see it, and it
// is not lost: it reappears as a fresh turn. A blocking tool holds the turn open
// so the test can inject deterministically.
func TestSteerMidTurnDeferredToOwnTurn(t *testing.T) {
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
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx) // drain dispatch/persist goroutines before TempDir removal
	})
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

	waitForTerminal(t, a, sid) // the "start" turn completes first

	// The continued "start" turn (request index 1, captured while the interjection
	// was still queued) must NOT absorb it as an instruction: the original prompt is
	// masked (deferred), and the text may appear ONLY inside the ephemeral
	// "magi runtime note" advisory (which tells the model it was queued and how to
	// route it) — never as a bare user message merged into the turn.
	rec.mu.Lock()
	if len(rec.reqs) < 2 {
		rec.mu.Unlock()
		t.Fatalf("expected >=2 model requests, got %d", len(rec.reqs))
	}
	for _, msg := range rec.reqs[1].Messages {
		text := joinPartText(msg.Parts)
		if strings.Contains(text, "ALSO_DO_THIS") && !strings.Contains(text, "magi runtime note") {
			rec.mu.Unlock()
			t.Fatalf("interjection merged into the in-flight turn outside the runtime note: %q", text)
		}
	}
	rec.mu.Unlock()

	// It must not be lost: the drain re-runs it as its own turn, producing a later
	// request that does contain it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec.mu.Lock()
		var ran bool
		for _, r := range rec.reqs[2:] {
			if requestContains(r, "ALSO_DO_THIS") {
				ran = true
				break
			}
		}
		n := len(rec.reqs)
		rec.mu.Unlock()
		if ran {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("deferred interjection never ran as its own turn (%d requests seen)", n)
		}
		time.Sleep(10 * time.Millisecond)
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
