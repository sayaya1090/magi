package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
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

// crowdTool is a read-only subagent that counts how many of it run at once.
type crowdTool struct {
	mu        sync.Mutex
	now, peak int
	done      atomic.Int32
}

func (t *crowdTool) Name() string            { return "scout" }
func (t *crowdTool) Description() string     { return "scout reads" }
func (t *crowdTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *crowdTool) Meta() port.ToolMetadata {
	return port.ToolMetadata{Subagent: true, ReadOnlyChildren: true}
}
func (t *crowdTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	t.mu.Lock()
	t.now++
	if t.now > t.peak {
		t.peak = t.now
	}
	t.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	t.mu.Lock()
	t.now--
	t.mu.Unlock()
	t.done.Add(1)
	return session.ToolResult{Content: json.RawMessage(`"ok"`)}, nil
}

// Read-only subagents in one step run at once, but each is a whole child turn against the backend:
// unbounded, ten of them are ten concurrent model streams. The core holds them to a few slots
// ([limits] max_parallel_children, default 4) and the rest WAIT for a slot rather than being
// refused — refusing would only make the model spend another step asking again.
func TestSubagentCallsInOneStepShareAFewSlots(t *testing.T) {
	run := func(cfg Config) (peak, done int) {
		t.Helper()
		store, _ := jsonl.New(t.TempDir())
		reg := builtin.Default()
		tool := &crowdTool{}
		reg.Register(tool)
		var step []port.ProviderEvent
		for i := 0; i < 6; i++ {
			step = append(step, port.ProviderEvent{Type: port.ProviderToolCall,
				ToolCall: &session.ToolCall{CallID: fmt.Sprintf("c_%d", i), Name: "scout", Args: json.RawMessage(fmt.Sprintf(`{"q":%d}`, i))}})
		}
		step = append(step, port.ProviderEvent{Type: port.ProviderFinish})
		llm := &scriptLLM{steps: [][]port.ProviderEvent{step, asideText("done")}}
		cfg.Permission = "allow"
		a := closeAfter(t, New(store, llm, reg, bus.New(), nil, cfg))
		ctx := context.Background()
		sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
		if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
			Parts: []session.Part{{Kind: session.PartText, Text: "look everywhere"}},
			Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
			t.Fatal(err)
		}
		waitForTerminal(t, a, sid)
		tool.mu.Lock()
		defer tool.mu.Unlock()
		return tool.peak, int(tool.done.Load())
	}
	if peak, done := run(Config{}); peak != 4 || done != 6 {
		t.Errorf("default: %d ran at once (want 4), %d of 6 finished", peak, done)
	}
	if peak, done := run(Config{MaxParallelChildren: 2}); peak != 2 || done != 6 {
		t.Errorf("max 2: %d ran at once (want 2), %d of 6 finished", peak, done)
	}
}
