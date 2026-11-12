package app

import (
	"context"
	"errors"
	"strings"
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

func flakyApp(t *testing.T, llm *flakyLLM) (*App, session.SessionID) {
	t.Helper()
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	return a, sid
}

func lastError(t *testing.T, evs []event.Event) event.ErrorData {
	t.Helper()
	var d event.ErrorData
	if !lastData(evs, event.TypeError, &d) {
		t.Fatalf("no error recorded; types=%v", typesOf(evs))
	}
	return d
}

// Count alone does not bound a ladder: the cumulative wait does too. When it is spent the turn
// stops as RESUMABLE \u2014 the conversation and every finished tool result stand, and the record says
// what was tried and how to carry on.
func TestWaitBudgetEndsTheLadderAsResumable(t *testing.T) {
	fastTransient(t)
	b := transientWaitBudget
	transientWaitBudget = 3 * time.Millisecond
	t.Cleanup(func() { transientWaitBudget = b })
	llm := &flakyLLM{failN: 1 << 30, msg: "llm: rate limited (status 429): slow down"}
	a, sid := flakyApp(t, llm)
	got := runToTerminal(t, a, sid)
	d := lastError(t, got)
	if !d.Resumable || d.Kind != "rate_limit" || d.Attempts < 1 || d.WaitedMs < 0 {
		t.Fatalf("want a resumable rate_limit stop with attempts recorded, got %+v", d)
	}
	if c := llm.callCount(); c >= maxTransientRetries+1 {
		t.Errorf("the wait budget should end the ladder before the count does; calls=%d", c)
	}
	if !strings.Contains(d.Message, "continue") {
		t.Errorf("the stop must say how to carry on: %q", d.Message)
	}
	if r, ok := a.ResumableTurnOf(context.Background(), sid); !ok || r.Kind != "rate_limit" {
		t.Errorf("the stopped turn should read as resumable: %+v ok=%v", r, ok)
	}
}

// A server that names a wait longer than the budget is not asked again early: no second call, no
// sleep, a resumable stop that carries the wait it named.
func TestServerWaitBeyondBudgetIsNotCalledEarly(t *testing.T) {
	fastTransient(t)
	llm := &flakyLLM{failN: 1 << 30, err: &port.BackendError{Kind: port.KindRateLimit, Status: 429,
		RetryAfter: time.Hour, RequestID: "req_7", Cause: errors.New("llm: rate limited (status 429): come back later")}}
	a, sid := flakyApp(t, llm)
	start := time.Now()
	got := runToTerminal(t, a, sid)
	if c := llm.callCount(); c != 1 {
		t.Errorf("want exactly 1 call when the server asks for longer than the budget, got %d", c)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("must not sit out the server's wait inside the turn: %v", el)
	}
	d := lastError(t, got)
	if !d.Resumable || d.RetryAfterMs != time.Hour.Milliseconds() || d.RequestID != "req_7" {
		t.Errorf("want resumable with the named wait and request id, got %+v", d)
	}
}

// A failure asking again cannot fix is recorded with its kind, and is not offered for resumption.
func TestPermanentFailureIsNotResumable(t *testing.T) {
	fastTransient(t)
	llm := &flakyLLM{failN: 1 << 30, msg: "llm: unauthorized \u2014 check the API key (MAGI_API_KEY) (status 401): bad key"}
	a, sid := flakyApp(t, llm)
	d := lastError(t, runToTerminal(t, a, sid))
	if d.Kind != "auth" || d.Resumable {
		t.Errorf("want kind auth, not resumable; got %+v", d)
	}
	if _, ok := a.ResumableTurnOf(context.Background(), sid); ok {
		t.Error("an auth failure is not resumable")
	}
}

// A stop pressed during a long wait ends it at once, with no further call to the backend.
func TestInterruptDuringBackoffEndsAtOnce(t *testing.T) {
	b, m := transientBackoffBase, transientBackoffMax
	transientBackoffBase, transientBackoffMax = time.Hour, time.Hour
	bb := transientWaitBudget
	transientWaitBudget = 10 * time.Hour
	t.Cleanup(func() { transientBackoffBase, transientBackoffMax, transientWaitBudget = b, m, bb })
	llm := &flakyLLM{failN: 1 << 30, msg: "llm: rate limited (status 429): slow down"}
	a, sid := flakyApp(t, llm)
	ctx := context.Background()
	a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "tui"}})
	for deadline := time.Now().Add(2 * time.Second); llm.callCount() < 1 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // into the wait
	start := time.Now()
	if err := a.Interrupt(ctx, command.Interrupt{SessionID: sid}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		evs, _ := a.store.Read(ctx, sid, 0)
		if countType(evs, event.TypeTurnFinished) >= 1 {
			if el := time.Since(start); el > time.Second {
				t.Errorf("stop took %v", el)
			}
			if c := llm.callCount(); c != 1 {
				t.Errorf("no call may follow a stop; calls=%d", c)
			}
			return
		}
	}
	t.Fatal("the turn did not end within 2s of the stop")
}
