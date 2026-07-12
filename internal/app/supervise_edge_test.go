package app

import (
	"context"
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

// busyNeverFinishLLM streams tokens forever on the first attempt but never sends
// finish — so the stall watchdog keeps re-arming on the activity and only the
// per-attempt SubagentTimeout can end it. On the restart it completes cleanly.
type busyNeverFinishLLM struct {
	mu   sync.Mutex
	call int
}

func (f *busyNeverFinishLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	n := f.call
	f.call++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		if n == 0 {
			tk := time.NewTicker(100 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tk.C:
					select {
					case ch <- port.ProviderEvent{Type: port.ProviderText, Text: "."}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "recovered"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	}()
	return ch, nil
}

func (f *busyNeverFinishLLM) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.call
}

// A subagent that stays active but never finishes must be ended by its per-attempt
// SubagentTimeout (not the stall watchdog, which its activity keeps re-arming) and
// then RESTARTED — the counterpart to the stall path (also retried) and the severed
// stream (not retried). Confirms the full supervisory retry taxonomy.
func TestSubagentTimeoutRestarted(t *testing.T) {
	t.Setenv("MAGI_SUBAGENT_JUDGE", "off") // deterministic-cap path under test; the judge would consume a scripted call
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &busyNeverFinishLLM{}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       10 * time.Second, // long: activity keeps the watchdog from firing
		SubagentTimeout:     2 * time.Second,  // short: the per-attempt deadline ends attempt 0
		SubagentMaxRestarts: 1,
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if res.Err != "" {
		t.Fatalf("expected recovery after the timeout restart, got error: %s", res.Err)
	}
	if !strings.Contains(res.Text, "recovered") {
		t.Fatalf("expected the restarted attempt's output, got %q", res.Text)
	}
	if n := llm.count(); n != 2 {
		t.Errorf("expected one timeout attempt + one recovery; got %d attempts", n)
	}
}

// countingDeadLLM hangs on every attempt and counts how many times it was called,
// so a test can prove the supervisor did NOT restart it.
type countingDeadLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *countingDeadLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

func (f *countingDeadLLM) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Cancelling the WHOLE run (parent ctx) while a subagent is in flight must unwind it
// promptly as a terminal cancellation — well before its stall/timeout — and must NOT
// restart it (a run abort is not a recoverable failure). Pins the ctx.Done branch of
// runAttempt, distinct from a child-specific interrupt.
func TestParentCancelSubagentNoRestart(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &countingDeadLLM{}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:       10 * time.Second,
		SubagentTimeout:     10 * time.Second,
		SubagentMaxRestarts: 2,
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan port.SpawnResult, 1)
	go func() {
		done <- a.spawn(ctx, parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	}()

	time.Sleep(100 * time.Millisecond) // let the attempt get underway
	cancel()

	select {
	case res := <-done:
		if res.Err == "" {
			t.Fatal("cancelling the whole run must surface an error, not a success")
		}
		if !strings.Contains(res.Err, "context canceled") {
			t.Errorf("want a cancellation error, got %q", res.Err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("spawn did not unwind promptly on parent cancel (waited for stall/timeout?)")
	}
	if n := llm.count(); n != 1 {
		t.Errorf("a cancelled run must not restart the subagent; got %d attempts", n)
	}
}
