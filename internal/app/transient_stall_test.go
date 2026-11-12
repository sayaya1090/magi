package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// wedgedLLM accepts every request and then says nothing until it is cancelled: a wedged backend.
type wedgedLLM struct{ calls atomic.Int32 }

func (s *wedgedLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	s.calls.Add(1)
	ch := make(chan port.ProviderEvent)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}

// A backend that stays silent past the first-token bound on every try is a transient failure like
// any other: after the bounded stall retries the step stops in the common shape — kind timeout,
// resumable, and saying how to carry on — not as a bare error with no way forward.
func TestASilentBackendEndsResumablyAsATimeout(t *testing.T) {
	oldS, oldF := streamStallTimeout, firstTokenTimeout
	streamStallTimeout, firstTokenTimeout = 30*time.Millisecond, 30*time.Millisecond
	defer func() { streamStallTimeout, firstTokenTimeout = oldS, oldF }()
	fastTransient(t)
	llm := &wedgedLLM{}
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	evs := runToTerminal(t, a, sid)

	if got, want := int(llm.calls.Load()), maxStreamStallRetries+1; got != want {
		t.Errorf("requests sent = %d, want %d (the stall retries, no more)", got, want)
	}
	d := lastError(t, evs)
	if d.Kind != string(port.KindTimeout) || !d.Resumable {
		t.Errorf("a silent backend should end resumably as a timeout; got kind=%q resumable=%v: %s", d.Kind, d.Resumable, d.Message)
	}
	if !strings.Contains(d.Message, `"continue"`) {
		t.Errorf("the record should say how to carry on: %s", d.Message)
	}
}
