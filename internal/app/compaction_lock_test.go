package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// blockLLM holds every summariser call until release is closed, so a compaction can be caught
// in the middle.
type blockLLM struct {
	started, release chan struct{}
	once             sync.Once
	mu               sync.Mutex
	calls            int
}

func (b *blockLLM) StreamChat(ctx context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.once.Do(func() { close(b.started) })
	ch := make(chan port.ProviderEvent, 2)
	go func() {
		select {
		case <-b.release:
		case <-ctx.Done():
		}
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "BRIEF"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
		close(ch)
	}()
	return ch, nil
}

func (b *blockLLM) callCount() int { b.mu.Lock(); defer b.mu.Unlock(); return b.calls }

// Once a compaction starts, no second one runs for the same session until it ends: a manual
// request (the button in any client, or /compact) is refused in words, and the automatic fold
// (per step, and the overflow retry) does nothing. When the first ends the lock is released.
func TestOnlyOneCompactionRunsAtATime(t *testing.T) {
	llm := &blockLLM{started: make(chan struct{}), release: make(chan struct{})}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "win", ContextWindow: 4000, Tools: true})
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow", Models: reg, CompactRatio: 0.5, CompactKeep: 0.25}))
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{
		Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "win"}})
	for i := 1; i <= 30; i++ {
		spoke(t, a, sid, fmt.Sprintf("m%d", i), strings.Repeat("x", 400))
	}
	first := make(chan error, 1)
	go func() { first <- a.Compact(ctx, command.Compact{SessionID: sid}) }()
	select {
	case <-llm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first compaction never reached the summariser")
	}

	// Each attempt runs off the test goroutine: without the lock it walks into the held summariser
	// and never comes back, which is the doubled compaction itself.
	soon := func(what string, f func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { f(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			close(llm.release)
			t.Fatalf("%s started a second compaction while one was running", what)
		}
	}
	var err error
	soon("a manual request", func() { err = a.Compact(ctx, command.Compact{SessionID: sid}) })
	if err == nil || !strings.Contains(err.Error(), "already running") {
		close(llm.release)
		t.Fatalf("a second manual compaction was not refused in words: %v", err)
	}
	var folded bool
	soon("the overflow fold", func() { folded = foldNow(t, a, sid) })
	if folded {
		t.Error("the overflow fold ran while a compaction was running")
	}
	soon("the per-step auto compaction", func() {
		evs, _ := a.store.Read(ctx, sid, 0)
		folded = a.maybeCompact(ctx, a.sessionInfo(ctx, sid), AgentSpec{Name: "default"}, event.Actor{}, evs, reconstruct(evs), "sys")
	})
	if folded {
		t.Error("the per-step auto compaction ran while a compaction was running")
	}
	if n := llm.callCount(); n != 1 {
		t.Errorf("summariser calls while one compaction ran = %d, want 1", n)
	}

	close(llm.release)
	if err := <-first; err != nil {
		t.Fatalf("the first compaction failed: %v", err)
	}
	for i := 31; i <= 40; i++ {
		spoke(t, a, sid, fmt.Sprintf("m%d", i), strings.Repeat("y", 400))
	}
	if err := a.Compact(ctx, command.Compact{SessionID: sid}); err != nil {
		t.Fatalf("after the first compaction ended the lock was not released: %v", err)
	}
}
