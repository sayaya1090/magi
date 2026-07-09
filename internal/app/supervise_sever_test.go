package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// severStreamLLM emits some partial text and then a ProviderError mid-stream —
// exactly the event shape the openai adapter produces when a backend stream is
// severed before finish_reason (consume's `default` branch). It counts calls so a
// test can prove the supervisor did NOT restart it.
type severStreamLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *severStreamLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 3)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "partial wo"}
	ch <- port.ProviderEvent{Type: port.ProviderError, Err: errors.New("unexpected EOF")}
	close(ch)
	return ch, nil
}

func (f *severStreamLLM) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// A subagent stream severed mid-flight (partial output, then a provider error) is
// a terminal failure, NOT a stall: it must be surfaced to the parent immediately
// as an error, without the restart the stall/timeout paths take. This pins the
// retry classification in runAttempt (a plain provider error → retry=false),
// which is what keeps a flaky backend from silently multiplying subagent work.
func TestSeveredSubagentStreamSurfacedWithoutRetry(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &severStreamLLM{}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       10 * time.Second, // large: the sever must win, not the watchdog
		SubagentTimeout:     10 * time.Second,
		SubagentMaxRestarts: 2, // a wrongful retry would show up as calls > 1
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	done := make(chan port.SpawnResult, 1)
	go func() {
		done <- a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	}()

	var res port.SpawnResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("spawn did not return promptly after a severed stream (retried/stalled?)")
	}

	if res.Err == "" {
		t.Fatal("a severed subagent stream must surface an error to the parent")
	}
	if !strings.Contains(res.Err, "provider error") {
		t.Errorf("error should be the provider-error surfaced by the loop, got %q", res.Err)
	}
	if n := llm.count(); n != 1 {
		t.Errorf("a provider error is not a stall/timeout and must NOT be retried; got %d attempts", n)
	}
}
