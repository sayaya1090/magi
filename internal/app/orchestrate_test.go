package app

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// gateLLM is a fake provider whose StreamChat blocks on a gate (to hold agents
// "running") and then returns a fixed text turn.
type gateLLM struct {
	active  atomic.Int32
	maxSeen atomic.Int32
	gate    chan struct{}
	text    string
}

func (f *gateLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	n := f.active.Add(1)
	for {
		m := f.maxSeen.Load()
		if n <= m || f.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	ch := make(chan port.ProviderEvent, 4)
	go func() {
		defer f.active.Add(-1)
		if f.gate != nil {
			select {
			case <-f.gate:
			case <-ctx.Done():
			}
		}
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: f.text}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
		close(ch)
	}()
	return ch, nil
}

func newOrchApp(t *testing.T, llm port.LLMProvider, cfg Config) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, llm, builtin.Default(), bus.New(), nil, cfg)
}

func parentSession(wd string) session.Session {
	return session.Session{ID: "s_parent", Workdir: wd, Agent: "default", Model: session.ModelRef{Model: "m"}}
}

// A spawned subagent runs and returns its final text.
func TestSpawnReturnsResult(t *testing.T) {
	llm := &gateLLM{text: "child result"}
	a := newOrchApp(t, llm, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"worker": {Name: "worker", System: "work"}},
	})
	a.mu.Lock()
	a.sessions["s_parent"] = parentSession(t.TempDir())
	a.mu.Unlock()

	res := a.spawn(context.Background(), parentSession(t.TempDir()), 0, port.SpawnRequest{Agent: "worker", Prompt: "do it"})
	if res.Err != "" {
		t.Fatalf("spawn error: %s", res.Err)
	}
	if res.Text != "child result" {
		t.Errorf("result=%q want 'child result'", res.Text)
	}
}

// Unknown agent is rejected.
func TestSpawnUnknownAgent(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow"})
	res := a.spawn(context.Background(), parentSession(t.TempDir()), 0, port.SpawnRequest{Agent: "ghost"})
	if res.Err == "" || !strings.Contains(res.Err, "unknown agent") {
		t.Errorf("expected unknown agent error, got %q", res.Err)
	}
}

// D7 rec-1: spawning beyond max depth is rejected.
func TestRecursionMaxDepth(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{
		Permission: "allow", MaxDepth: 3,
		Agents: map[string]AgentSpec{"w": {Name: "w", System: "w"}},
	})
	// depth == MaxDepth means depth+1 > MaxDepth → rejected.
	res := a.spawn(context.Background(), parentSession(t.TempDir()), 3, port.SpawnRequest{Agent: "w"})
	if res.Err == "" || !strings.Contains(res.Err, "max depth") {
		t.Errorf("expected max depth error, got %q", res.Err)
	}
}

// D7 rec-3: cumulative spawn budget is enforced.
func TestRecursionCumulativeBudget(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{
		Permission: "allow", MaxAgents: 3,
		Agents: map[string]AgentSpec{"w": {Name: "w", System: "w"}},
	})
	ok := 0
	var budget int
	for i := 0; i < 5; i++ {
		res := a.spawn(context.Background(), parentSession(t.TempDir()), 0, port.SpawnRequest{Agent: "w"})
		if res.Err == "" {
			ok++
		} else if strings.Contains(res.Err, "budget") {
			budget++
		}
	}
	if ok != 3 || budget != 2 {
		t.Errorf("ok=%d budget=%d, want 3 and 2", ok, budget)
	}
}

// D7 rec-2: concurrency is capped; the Nth+1 concurrent spawn queues until a
// slot frees (it does not error).
func TestRecursionConcurrencyCap(t *testing.T) {
	gate := make(chan struct{})
	llm := &gateLLM{text: "ok", gate: gate}
	a := newOrchApp(t, llm, Config{
		Permission: "allow", Concurrency: 2, MaxAgents: 100,
		Agents: map[string]AgentSpec{"w": {Name: "w", System: "w"}},
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.spawn(context.Background(), parentSession(t.TempDir()), 0, port.SpawnRequest{Agent: "w"})
		}()
	}

	// Let goroutines pile up against the gate, then release.
	time.Sleep(200 * time.Millisecond)
	if got := llm.maxSeen.Load(); got > 2 {
		t.Errorf("max concurrent agents=%d, want <= 2 (Concurrency cap)", got)
	}
	close(gate)
	wg.Wait()
}
