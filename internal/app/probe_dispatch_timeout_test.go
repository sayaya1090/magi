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
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// hangCountLLM hangs every call until its ctx dies, counting the calls. A hung
// call keeps the child event-quiet, so with a generous SubagentStall the only
// thing that can end it is a timeout — which lets these tests distinguish the
// per-attempt SubagentTimeout (retried) from the overall SpawnRequest.Timeout
// (terminal) by how many calls were made.
type hangCountLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *hangCountLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent)
	go func() { defer close(ch); <-ctx.Done() }()
	return ch, nil
}

func (f *hangCountLLM) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newDispatchTimeoutApp(t *testing.T, llm port.LLMProvider, subTimeout time.Duration) (*App, session.Session) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{
		Permission:          "allow",
		Agents:              map[string]AgentSpec{"explore": {Name: "explore"}},
		SubagentStall:       time.Hour, // never stall-kill: only timeouts end a hung child here
		SubagentTimeout:     subTimeout,
		SubagentMaxRestarts: 2,
	})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	parent := a.stateLocked(sid).meta
	a.mu.Unlock()
	return a, parent
}

// waitBGDone polls until the parent's background group has no outstanding spawns.
func waitBGDone(t *testing.T, a *App, sid session.SessionID, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		n := a.bgFor(sid).outstanding
		a.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("background spawn still outstanding after %v", within)
}

// A SpawnRequest.Timeout bounds the WHOLE background spawn: expiry is a parent-ctx
// cancellation, so the attempt is not retried — one LLM call, prompt ended well
// before the (huge) per-attempt SubagentTimeout or any restart could run.
func TestDispatchOverallTimeoutIsTerminal(t *testing.T) {
	llm := &hangCountLLM{}
	a, parent := newDispatchTimeoutApp(t, llm, time.Hour)

	msg := a.dispatch(context.Background(), parent, 0, port.SpawnRequest{
		Agent: "explore", Prompt: "investigate", Timeout: 200 * time.Millisecond,
	})
	if strings.Contains(msg, "not re-dispatched") {
		t.Fatalf("fresh dispatch was refused: %q", msg)
	}
	waitBGDone(t, a, parent.ID, 5*time.Second)

	if got := llm.count(); got != 1 {
		t.Fatalf("overall timeout must be terminal (no restart attempts): got %d LLM calls, want 1", got)
	}
}

// Without an overall Timeout the per-attempt SubagentTimeout still retries: the
// supervisor restarts a timed-out attempt up to SubagentMaxRestarts times.
func TestDispatchPerAttemptTimeoutStillRetries(t *testing.T) {
	t.Setenv("MAGI_SUBAGENT_JUDGE", "off") // judge would route to the same hanging LLM
	llm := &hangCountLLM{}
	a, parent := newDispatchTimeoutApp(t, llm, 150*time.Millisecond)

	a.dispatch(context.Background(), parent, 0, port.SpawnRequest{Agent: "explore", Prompt: "investigate"})
	waitBGDone(t, a, parent.ID, 10*time.Second)

	if got := llm.count(); got != 3 {
		t.Fatalf("per-attempt timeout should retry (1 + SubagentMaxRestarts): got %d LLM calls, want 3", got)
	}
}
