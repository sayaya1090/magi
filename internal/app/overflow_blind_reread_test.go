package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// blindAfterCompaction reads normally until a compaction is written, and cannot read after that.
//
// Keyed on CONTENT rather than on a call count. The read under test is the one the overflow
// recovery does immediately after folding, and a fixture that counted calls would drift onto a
// different read the first time anything upstream reads once more or once less.
type blindAfterCompaction struct {
	port.Store
	blind atomic.Bool
}

func (b *blindAfterCompaction) Append(ctx context.Context, sid session.SessionID, evs ...event.Event) ([]int64, error) {
	out, err := b.Store.Append(ctx, sid, evs...)
	for _, e := range evs {
		if e.Type == event.TypeCompaction {
			b.blind.Store(true)
		}
	}
	return out, err
}

func (b *blindAfterCompaction) Read(ctx context.Context, sid session.SessionID, from int64) ([]event.Event, error) {
	if b.blind.Load() {
		return nil, errors.New("the log is on a disk that went away")
	}
	return b.Store.Read(ctx, sid, from)
}

// recordingOverflowLLM is overflowLLM that also remembers how big each request was.
type recordingOverflowLLM struct {
	mu    sync.Mutex
	calls int
	msgs  []int
}

func (f *recordingOverflowLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.msgs = append(f.msgs, len(r.Messages))
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

func (f *recordingOverflowLLM) sizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.msgs...)
}

// An overflow recovery that cannot re-read the folded log must not ask the model nothing.
//
// The recovery is: the backend refused the request for size, so fold the history and re-issue.
// Re-issuing needs the folded log, and the read that fetches it discarded its error. Read answers
// (nil, err) when the log cannot be read, so an unreadable log rebuilt the request from no
// conversation at all. Measured before the fix, end to end: the retry went out with ZERO messages,
// the backend answered it, and the turn recorded turn.finished with no error — the recovery for
// "this request was too big" was to ask the model nothing and call the reply an answer.
//
// The failure is staged after the fold rather than everywhere, because a store that never reads
// would fail the turn long before this line and prove nothing about it.
func TestAnOverflowRetryNeverAsksTheModelNothing(t *testing.T) {
	ctx := context.Background()
	inner, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &blindAfterCompaction{Store: inner}
	llm := &recordingOverflowLLM{}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, _ := a.CreateSession(ctx, command.CreateSession{
		Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})

	big := strings.Repeat("x", 50)
	for i := 0; i < 10; i++ { // > keepRecentEvents+1, so there is something to fold
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: big}})
		a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{}, d)
	}

	got := runToTerminal(t, a, sid)

	sizes := llm.sizes()
	if len(sizes) == 0 {
		t.Fatal("the provider was never called, so the overflow path never ran")
	}
	for i, n := range sizes {
		if n == 0 {
			t.Errorf("request %d of %d went out with no messages at all — built from a log that "+
				"could not be read; sizes=%v", i+1, len(sizes), sizes)
		}
	}
	// The fold happened (so the recovery really was entered) and the failure it could not recover
	// from is reported rather than dressed as a finished turn.
	if countType(got, event.TypeCompaction) == 0 {
		t.Fatalf("the recovery never folded, so this asserts nothing about it; types=%v", typesOf(got))
	}
	if countType(got, event.TypeError) == 0 {
		t.Errorf("the retry could not be built and nothing said so; types=%v", typesOf(got))
	}
}
