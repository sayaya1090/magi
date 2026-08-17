package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func TestDegenerateRepeat(t *testing.T) {
	cases := []struct {
		name string
		tail string
		want bool // whether a repetition loop is detected
	}{
		{"sentence repeated", strings.Repeat("The server is now running on port 5328. ", 6), true},
		{"short phrase repeated", strings.Repeat("the ", 60), true},
		{"single char run", strings.Repeat("a", 200), true},
		{"normal prose", "The quick brown fox jumps over the lazy dog. " +
			"A completely ordinary paragraph of varied text that does not loop at all, continuing with more words.", false},
		{"realistic planner JSON (must not false-fire)", `{"steps":[` +
			`{"n":1,"strategy":"solo","title":"Read and analyze the OCaml runtime shared_heap.c"},` +
			`{"n":2,"strategy":"solo","title":"Locate the POOL_BLOCK_FREE_HP macro and its callers"},` +
			`{"n":3,"strategy":"solo","title":"Implement the run-length compression fix in the free-list walk"},` +
			`{"n":4,"strategy":"solo","title":"Build the runtime and run the GC stress test to verify"}]}`, false},
		{"blank lines only (not content)", strings.Repeat("\n", 200), false},
		{"too short to judge", "the the the", false},
		{"few reps below threshold", strings.Repeat("hello world ", 2), false}, // 24 bytes < repMinBlock
	}
	for _, c := range cases {
		got := degenerateRepeat([]byte(c.tail)) > 0
		if got != c.want {
			t.Errorf("%s: degenerateRepeat=%v, want %v", c.name, got, c.want)
		}
	}
}

// A non-repeating tail must stay cheap: each candidate period mismatches at the first comparison.
func TestDegenerateRepeatNoFalsePositiveOnVariedText(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		// The line number makes the content non-periodic (no repeating block), unlike a fixed cycle.
		fmt.Fprintf(&b, "line %d: %s done\n", i, strings.Repeat("x", i%13))
	}
	if p := degenerateRepeat([]byte(b.String())); p > 0 {
		t.Errorf("varied text falsely flagged as repetition (period %d)", p)
	}
}

// GuardProvider is idempotent (double-wrap returns the same guarded provider, never a nested one) and
// nil-safe, so applying it at every provider-creation site is cheap and cannot double-count the guard.
func TestGuardProviderIdempotentAndNilSafe(t *testing.T) {
	if GuardProvider(nil) != nil {
		t.Error("GuardProvider(nil) must stay nil")
	}
	g1 := GuardProvider(noopProvider{})
	if _, ok := g1.(guardedProvider); !ok {
		t.Fatalf("GuardProvider must return a guardedProvider, got %T", g1)
	}
	if g2 := GuardProvider(g1); g2 != g1 {
		t.Error("wrapping an already-guarded provider must return it unchanged, not double-wrap")
	}
}

// noopProvider is a minimal port.LLMProvider for the wrapping tests; its StreamChat is never invoked.
type noopProvider struct{}

func (noopProvider) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	return nil, nil
}

// spinProvider streams a repeating unit forever until its context is cancelled — the shape the
// repetition backstop exists for.
type spinProvider struct{ unit string }

func (p spinProvider) StreamChat(ctx context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		for {
			select {
			case ch <- port.ProviderEvent{Type: port.ProviderText, Text: p.unit}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// When the guard stops a stream it must SAY SO in the stream: one error carrying ErrStreamAborted,
// so the loop can tell magi's own intervention from a dead connection. Cancelling alone reached the
// loop as a bare "context canceled" — a failed request — and the safety net ended the run it was
// meant to save. The repeated unit rides the message because the guard's evidence otherwise dies
// with the stream: deltas are transient and an aborted reply is never appended, leaving a log that
// says a loop happened and cannot say what looped.
func TestTheGuardSaysItStoppedTheStreamAndWhat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := GuardProvider(spinProvider{unit: "the server is now running on port 5328. "}).
		StreamChat(ctx, port.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var gotErr error
	for ev := range ch {
		if ev.Type == port.ProviderError {
			gotErr = ev.Err
		}
	}
	if gotErr == nil {
		t.Fatal("the guard cut the stream without a word — the loop cannot tell that from a dropped connection")
	}
	if !errors.Is(gotErr, port.ErrStreamAborted) {
		t.Errorf("the abort must carry ErrStreamAborted, got %v", gotErr)
	}
	if !strings.Contains(gotErr.Error(), "repetition loop") {
		t.Errorf("the abort must name what it saw, got %v", gotErr)
	}
	if !strings.Contains(gotErr.Error(), "port 5328") {
		t.Errorf("the abort must carry the repeated unit as evidence, got %v", gotErr)
	}
}

// deafProvider streams a repeating unit WITHOUT ever selecting on its context — the
// least-cooperative producer GuardProvider's "every model request" contract must still not leak.
type deafProvider struct {
	sends int32
	done  chan struct{}
}

func (p *deafProvider) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		defer close(p.done)
		for i := 0; i < 5000; i++ {
			ch <- port.ProviderEvent{Type: port.ProviderText, Text: "the same unit over and over. "}
			atomic.AddInt32(&p.sends, 1)
		}
	}()
	return ch, nil
}

// After an abort the guard must go on draining the producer: before abort() existed the guard
// loop read `inner` to its close after a cancel, and returning without a drain narrowed that to
// "your producer must be cancel-aware" — enforced nowhere, stated nowhere, and a deaf producer
// blocked on its send forever.
func TestTheGuardDrainsADeafProducerAfterAborting(t *testing.T) {
	p := &deafProvider{done: make(chan struct{})}
	ch, err := GuardProvider(p).StreamChat(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	sawAbort := false
	for ev := range ch {
		if ev.Type == port.ProviderError && errors.Is(ev.Err, port.ErrStreamAborted) {
			sawAbort = true
		}
	}
	if !sawAbort {
		t.Fatal("the repetition guard never fired on a pure repetition stream")
	}
	select {
	case <-p.done: // every send got through: the producer was drained to completion, not stranded
	case <-time.After(5 * time.Second):
		t.Fatalf("producer still blocked after the abort (%d/5000 sends) — the drain is gone",
			atomic.LoadInt32(&p.sends))
	}
}
