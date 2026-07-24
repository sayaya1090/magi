package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The classifier must fire on the varied ways backends phrase a context-length rejection, and
// stay quiet on unrelated failures — an unrelated 400 or a network drop must NOT trigger a
// compaction-and-retry (that would mask a real error as a size problem).
func TestIsContextOverflow(t *testing.T) {
	hits := []error{
		errors.New("This model's maximum context length is 4096 tokens, however you requested 5200 tokens"),
		errors.New("openai: 400 code=context_length_exceeded"),
		errors.New("Please reduce the length of the messages"),
		errors.New("prompt is too long: 210000 tokens > 200000 maximum"),
		errors.New("input is too long for the model"),
	}
	for _, e := range hits {
		if !isContextOverflow(e) {
			t.Errorf("should classify as overflow: %v", e)
		}
	}
	misses := []error{
		nil,
		errors.New("connection refused"),
		errors.New("500 internal server error"),
		errors.New("model not found"),
	}
	for _, e := range misses {
		if isContextOverflow(e) {
			t.Errorf("should NOT classify as overflow: %v", e)
		}
	}
}

// compactNow folds older history into a summary even when the ratio gate would never trip — here
// the model has no registered window (maybeCompact would no-op), yet the forced path still writes
// a compaction event because there are more facts than the kept tail.
func TestCompactNowFoldsRegardlessOfBudget(t *testing.T) {
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	llm := &usageLLM{text: "SUMMARY"} // serves as the summarizer
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})

	big := strings.Repeat("x", 50)
	for i := 0; i < 10; i++ { // > keepRecentEvents+1, so there is something to fold
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: big}})
		a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{}, d)
	}
	evs, _ := a.store.Read(ctx, sid, 0)
	s := a.sessionInfo(ctx, sid)

	if !a.compactNow(ctx, s, AgentSpec{Name: "default"}, event.Actor{}, evs) {
		t.Fatal("compactNow should fold when facts exceed the kept tail, regardless of window")
	}
	after, _ := a.store.Read(ctx, sid, 0)
	if n := countType(after, event.TypeCompaction); n != 1 {
		t.Errorf("want 1 compaction event, got %d (types %v)", n, typesOf(after))
	}
}

// overflowLLM rejects the FIRST StreamChat as too long, then behaves normally — modeling a backend
// whose real window is smaller than magi assumed. Subsequent calls (the forced summarize + the
// retried generate) succeed.
type overflowLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *overflowLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	if n == 1 {
		return nil, errors.New("This model's maximum context length is 4096 tokens, however you requested 5200 tokens")
	}
	ch := make(chan port.ProviderEvent, 4)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "ok"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (f *overflowLLM) callCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

// End-to-end: a context-overflow rejection is recovered by force-compacting and re-issuing, so the
// turn FINISHES rather than dying with a terminal error — and a compaction event is recorded.
func TestContextOverflowCompactsAndRetries(t *testing.T) {
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	llm := &overflowLLM{}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})

	big := strings.Repeat("x", 50)
	for i := 0; i < 10; i++ {
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: big}})
		a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{}, d)
	}

	got := runToTerminal(t, a, sid)
	if n := countType(got, event.TypeError); n != 0 {
		t.Fatalf("overflow should have recovered, but got %d terminal error(s); types=%v", n, typesOf(got))
	}
	if countType(got, event.TypeCompaction) == 0 {
		t.Errorf("expected a forced compaction on overflow; types=%v", typesOf(got))
	}
	if !lastData(got, event.TypeTurnFinished, &event.TurnFinishedData{}) {
		t.Error("turn did not finish after recovery")
	}
	if c := llm.callCount(); c < 3 {
		t.Errorf("expected >=3 provider calls (generate → summarize → retry), got %d", c)
	}
}

// With the safety net off, the same overflow surfaces immediately as a terminal error — no
// compaction, no retry (fail-fast, for A/B).
func TestContextOverflowFlagOffFailsFast(t *testing.T) {
	t.Setenv("MAGI_CTX_COMPACT_RETRY", "0")
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	llm := &overflowLLM{}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})

	big := strings.Repeat("x", 50)
	for i := 0; i < 10; i++ {
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m", Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: big}})
		a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{}, d)
	}

	got := runToTerminal(t, a, sid)
	if countType(got, event.TypeCompaction) != 0 {
		t.Error("flag off must NOT compact-retry")
	}
	if countType(got, event.TypeError) == 0 {
		t.Errorf("flag off must surface the overflow as a terminal error; types=%v", typesOf(got))
	}
	if c := llm.callCount(); c != 1 {
		t.Errorf("flag off must not retry; provider calls=%d, want 1", c)
	}
}
