package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// interleaveLLM models an orchestrator that, after dispatching a BACKGROUND
// subagent, keeps doing its OWN independent work (a file write) instead of
// parking immediately. The worker blocks until the test releases it, so the
// orchestrator's own work provably completes WHILE the subagent is still running.
type interleaveLLM struct{ release chan struct{} }

func (f *interleaveLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	last := lastMsgText(r)
	ch := make(chan port.ProviderEvent, 4)
	emit := func(evs ...port.ProviderEvent) {
		for _, e := range evs {
			ch <- e
		}
		close(ch)
	}
	switch {
	case strings.Contains(last, "All subagents have reported"), strings.Contains(last, "[subagent worker result]"):
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "synthesis"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "own_work"):
		// Saw its own write complete; nothing left but the subagent result → park.
		emit(port.ProviderEvent{Type: port.ProviderText, Text: "did my own work; waiting for worker"}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "Dispatched"):
		// Independent work while the worker runs in the background.
		emit(port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "c_write", Name: "write",
			Args: json.RawMessage(`{"path":"own_work.txt","content":"orchestrator output"}`),
		}}, port.ProviderEvent{Type: port.ProviderFinish})
	case strings.Contains(last, "WORKER_TASK"):
		go func() {
			defer close(ch)
			select {
			case <-f.release:
			case <-ctx.Done():
				return
			}
			ch <- port.ProviderEvent{Type: port.ProviderText, Text: "worker output"}
			ch <- port.ProviderEvent{Type: port.ProviderFinish}
		}()
	default: // dispatch the worker in the background
		emit(port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "c_task", Name: "task",
			Args: json.RawMessage(`{"agent":"worker","prompt":"WORKER_TASK"}`),
		}}, port.ProviderEvent{Type: port.ProviderFinish})
	}
	return ch, nil
}

// lastMsgText returns the last state-bearing text in the request. The loop appends
// an ephemeral volatile block (step budget, todos) as a trailing user message on
// every step — that block is pacing metadata, not conversation state, so it is
// skipped: the marker a routing fake dispatches on lives in the message before it.
func lastMsgText(r port.ChatRequest) string {
	last := ""
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			if p.Kind == session.PartText && p.Text != "" && !strings.Contains(p.Text, "# Step budget") {
				last = p.Text
			}
			if p.Kind == session.PartToolResult && p.ToolResult != nil {
				var s string
				if json.Unmarshal(p.ToolResult.Content, &s) == nil {
					last = s
				} else {
					last = string(p.ToolResult.Content)
				}
			}
		}
	}
	return last
}

// The orchestrator does its own work while a background subagent is still
// running (it is not blocked by the dispatch), then synthesizes once the result
// arrives — proving interleaving.
func TestOrchestratorInterleavesOwnWork(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	llm := &interleaveLLM{release: make(chan struct{})}
	a := New(store, llm, reg, bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       3 * time.Second,
		SubagentTimeout:     10 * time.Second,
		SubagentMaxRestarts: 0,
	})
	ctx := context.Background()
	wd := t.TempDir()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "delegate to worker"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	// The orchestrator's own file must appear WHILE the worker is still blocked.
	ownFile := filepath.Join(wd, "own_work.txt")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ownFile); err == nil {
			break // orchestrator did independent work without waiting for the worker
		}
		if time.Now().After(deadline) {
			t.Fatal("orchestrator did not do its own work while the subagent ran (no interleaving)")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now let the worker finish; the turn should complete and inject the result.
	close(llm.release)
	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) < 1 {
		t.Fatalf("expected the turn to finish, got %v", typesOf(got))
	}
	evs, _ := store.Read(ctx, sid, 0)
	injected := false
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted || e.Actor.Kind != event.ActorAgent {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		for _, p := range d.Parts {
			if strings.Contains(p.Text, "worker output") {
				injected = true
			}
		}
	}
	if !injected {
		t.Fatal("expected the worker's result (\"worker output\") to be injected after release")
	}
}
