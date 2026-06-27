package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// emptyThenAnswerLLM mimics a model that, after a tool result, returns an EMPTY
// turn (no text, no tool calls) — then, once nudged, produces its answer.
type emptyThenAnswerLLM struct {
	mu sync.Mutex
	n  int
}

func (f *emptyThenAnswerLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	n := f.n
	f.n++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 4)
	switch n {
	case 0: // first turn: call a read-only tool
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_list", Name: "list", Args: []byte(`{"path":"."}`)}}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	case 1: // after the tool result: go silent (empty turn)
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	default: // after the empty-result nudge: deliver the answer
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "my findings"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	}
	close(ch)
	return ch, nil
}

// A subagent that goes silent after a tool result is nudged once to produce its
// result, so the orchestrator gets a real answer instead of an empty one.
func TestSubagentEmptyResultNudged(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, &emptyThenAnswerLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"worker": {Name: "worker", Tools: []string{"read", "list"}}},
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "do the task"})
	if res.Err != "" {
		t.Fatalf("unexpected err: %s", res.Err)
	}
	if !strings.Contains(res.Text, "my findings") {
		t.Errorf("expected the nudged answer, got %q", res.Text)
	}
}
