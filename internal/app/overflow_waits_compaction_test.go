package app

import (
	"context"
	"encoding/json"
	"errors"
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

// heldFoldLLM holds every summariser call (the fold prompt) until release, refuses the first
// ordinary request as too long, and answers later ones.
type heldFoldLLM struct {
	foldStarted, release, refused chan struct{}
	foldOnce, refuseOnce          sync.Once
	mu                            sync.Mutex
	asks                          int
}

func (f *heldFoldLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	if strings.Contains(r.System, "You are folding the older part") {
		f.foldOnce.Do(func() { close(f.foldStarted) })
		go func() {
			select {
			case <-f.release:
			case <-ctx.Done():
			}
			ch <- port.ProviderEvent{Type: port.ProviderText, Text: "BRIEF"}
			ch <- port.ProviderEvent{Type: port.ProviderFinish}
			close(ch)
		}()
		return ch, nil
	}
	f.mu.Lock()
	f.asks++
	n := f.asks
	f.mu.Unlock()
	if n == 1 {
		f.refuseOnce.Do(func() { close(f.refused) })
		return nil, errors.New("This model's maximum context length is 4096 tokens, however you requested 5200 tokens")
	}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "ok"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A request refused for size while a manual compaction runs does not end the turn: the overflow
// retry waits for that compaction (it is not "nothing more to fold") and re-issues the request from
// the folded log.
func TestAnOverflowDuringAManualCompactionWaitsForIt(t *testing.T) {
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	llm := &heldFoldLLM{foldStarted: make(chan struct{}), release: make(chan struct{}), refused: make(chan struct{})}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	for i := 0; i < 10; i++ {
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: strings.Repeat("x", 50)}})
		a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{}, d)
	}
	manual := make(chan error, 1)
	go func() { manual <- a.Compact(ctx, command.Compact{SessionID: sid}) }()
	select {
	case <-llm.foldStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the manual compaction never reached the summariser")
	}
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "go on"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		close(llm.release)
		t.Fatal(err)
	}
	select {
	case <-llm.refused:
	case <-time.After(5 * time.Second):
		close(llm.release)
		t.Fatal("the turn's request was never refused")
	}
	finished := func() []event.Event {
		evs, _ := a.store.Read(ctx, sid, 0)
		return evs
	}
	time.Sleep(500 * time.Millisecond)
	if evs := finished(); countType(evs, event.TypeTurnFinished) > 0 {
		close(llm.release)
		t.Fatalf("the turn ended while the manual compaction was still running; errors=%d", countType(evs, event.TypeError))
	}
	close(llm.release)
	if err := <-manual; err != nil {
		t.Fatalf("the manual compaction failed: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && countType(finished(), event.TypeTurnFinished) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	evs := finished()
	if countType(evs, event.TypeTurnFinished) == 0 {
		t.Fatal("the turn never finished after the compaction ended")
	}
	if n := countType(evs, event.TypeError); n != 0 {
		t.Errorf("the overflow was not recovered: %d error(s)", n)
	}
	if n := countType(evs, event.TypeCompaction); n != 1 {
		t.Errorf("compactions = %d, want 1 (the manual one, reused by the retry)", n)
	}
}

// Waiting is bounded by the turn: cancelling it stops the wait.
func TestWaitingForACompactionStopsWithTheTurn(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, &usageLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if !a.beginCompaction(sid) {
		t.Fatal("lock not free")
	}
	defer a.endCompaction(sid)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if a.waitCompaction(ctx, sid) {
		t.Error("a cancelled wait reported the compaction finished")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("the wait ignored its context for %s", d)
	}
}
