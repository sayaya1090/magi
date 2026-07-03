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

// stallingLLM hangs (emits nothing) on the first call so the stall watchdog
// fires, then succeeds on the restart.
type stallingLLM struct {
	mu   sync.Mutex
	call int
}

func (f *stallingLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	n := f.call
	f.call++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		if n == 0 {
			<-ctx.Done() // hang with no activity → watchdog should restart us
			return
		}
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "recovered"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	}()
	return ch, nil
}

// A stalled subagent is detected and restarted; the restart succeeds.
func TestSupervisorRestartsStalledSubagent(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, &stallingLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       150 * time.Millisecond,
		SubagentTimeout:     10 * time.Second,
		SubagentMaxRestarts: 2,
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if res.Err != "" {
		t.Fatalf("expected recovery, got error: %s", res.Err)
	}
	if !strings.Contains(res.Text, "recovered") {
		t.Fatalf("expected restarted attempt's output, got %q", res.Text)
	}
}

// A subagent that never makes progress exhausts its restarts and returns an error
// (rather than hanging the system forever).
func TestSupervisorGivesUpAfterRestarts(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, &deadLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       100 * time.Millisecond,
		SubagentTimeout:     5 * time.Second,
		SubagentMaxRestarts: 1,
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if res.Err == "" {
		t.Fatal("expected an error after exhausting restarts")
	}
	if !strings.Contains(res.Err, "attempts") {
		t.Fatalf("error should note attempts exhausted, got %q", res.Err)
	}
}

// Interrupting a specific subagent cancels just that one promptly (well before
// its stall/timeout) and returns an error without restarting.
func TestInterruptSpecificSubagent(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, &deadLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       10 * time.Second,
		SubagentTimeout:     30 * time.Second,
		SubagentMaxRestarts: 2,
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}
	ctx := context.Background()

	ch, cancelSub, err := a.Subscribe(ctx, parent.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()

	resCh := make(chan port.SpawnResult, 1)
	go func() {
		resCh <- a.spawn(ctx, parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	}()

	// Capture the child session id from the spawn announcement, then interrupt it.
	var childID string
	for e := range ch {
		if e.Type == event.TypeAgentSpawned {
			var d event.AgentStatusData
			_ = json.Unmarshal(e.Data, &d)
			childID = d.AgentID
			break
		}
	}
	if childID == "" {
		t.Fatal("never saw the subagent spawn")
	}
	a.Interrupt(ctx, command.Interrupt{SessionID: session.SessionID(childID)})

	select {
	case res := <-resCh:
		if res.Err == "" {
			t.Fatal("expected an error after interrupting the subagent")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("spawn did not return promptly after interrupt")
	}
}

// deadLLM always hangs (never makes progress) — every attempt stalls.
type deadLLM struct{}

func (deadLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

// Async dispatch: the task tool returns immediately, the subagent runs in the
// background, and its result is injected into the parent session and processed
// by the orchestrator — all in one continuous turn.
func TestAsyncDispatchInjectsResult(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	a := New(store, &routingLLM{}, reg, bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       2 * time.Second,
		SubagentTimeout:     5 * time.Second,
		SubagentMaxRestarts: 1,
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "delegate to worker"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})

	got := waitForTerminal(t, a, sid)
	if countType(got, event.TypeTurnFinished) < 1 {
		t.Fatalf("expected the turn to finish, got %v", typesOf(got))
	}
	// The worker's ACTUAL result ("worker output") must have been injected into the
	// parent session — not merely some agent-authored prompt (an empty/wrong injection
	// would otherwise pass).
	evs, _ := store.Read(ctx, sid, 0)
	found := false
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
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected the worker's result (\"worker output\") to be injected into the parent session")
	}
}

// routingLLM routes by message content: orchestrator dispatches, worker produces
// output, orchestrator finishes after seeing the injected result.
type routingLLM struct{}

func (routingLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	last := ""
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			// Skip the ephemeral volatile block (step budget) — see lastMsgText.
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
	ch := make(chan port.ProviderEvent, 4)
	switch {
	case strings.Contains(last, "All subagents have reported"):
		// The synthesis nudge → final answer, no tool calls.
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "synthesis"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
		close(ch)
		return ch, nil
	case strings.Contains(last, "[subagent worker result]"):
		// Orchestrator saw the injected result → finish.
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "all done"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	case strings.Contains(last, "Dispatched"):
		// Orchestrator acknowledged dispatch and produces NO tool call, so the loop
		// reaches the finish branch and waits for the background result.
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "started, will report back"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	case strings.Contains(last, "WORKER_TASK"):
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "worker output"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	default: // orchestrator's first step: dispatch the worker
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "c_task", Name: "task",
			Args: mustJSON2(`{"agent":"worker","prompt":"WORKER_TASK"}`),
		}}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	}
	close(ch)
	return ch, nil
}

func mustJSON2(s string) []byte { return []byte(s) }
