package app

import (
	"context"
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

// SPEC F-EVENT-FACT-TRANSIENT `seq-1`: a fact type does not promise a seq on every frame.
//
// SetModel takes two exits out of one call — appendFact when the App has a Store, publishTransient
// when it does not — so `model.changed` reaches the bus stamped in one arrangement and at Seq 0 in
// the other. A client that decides "this type is persisted, therefore this frame has a position"
// and advances a cursor by type is wrong exactly in the second case, and nothing about the type
// tells it which case it is in. R4 says to read seq off the envelope; this holds R4 to the code.
//
// It is not asking for the two exits to be merged. A store-less App is deliberate — the doubles in
// tests and the read-only paths that never write a log — and a routing change is not a reason for
// the process to die. What must stay true is that the difference is visible in the envelope.
func TestModelChangedCarriesSeqOnlyWhenItWasStored(t *testing.T) {
	seqOf := func(t *testing.T, withStore bool) int64 {
		t.Helper()
		var st port.Store // a nil INTERFACE, not a typed nil — routing.go asks `a.store == nil`
		if withStore {
			s, _ := jsonl.New(t.TempDir())
			st = s
		}
		b := bus.New()
		a := New(st, &fakeLLM{}, builtin.Default(), b, nil, Config{
			Model: session.ModelRef{Provider: "openai", Model: "base-model"},
		})
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = a.Close(ctx)
		})
		sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
		if err != nil {
			t.Fatalf("CreateSession(withStore=%v): %v", withStore, err)
		}
		subCtx, cancelSub := context.WithCancel(context.Background())
		defer cancelSub()
		sub, unsub := b.Subscribe(subCtx, sid)
		defer unsub()

		a.SetModel(sid, "new-model")

		deadline := time.After(3 * time.Second)
		for {
			select {
			case e := <-sub:
				if e.Type != event.TypeModelChanged {
					continue
				}
				return e.Seq
			case <-deadline:
				t.Fatalf("no model.changed on the bus (withStore=%v)", withStore)
			}
		}
	}

	if seq := seqOf(t, true); seq == 0 {
		t.Errorf("model.changed with a Store carries Seq 0 — it was appended, so it has a position")
	}
	if seq := seqOf(t, false); seq != 0 {
		t.Errorf("model.changed without a Store carries Seq %d — nothing wrote it down, so it has no position", seq)
	}
}
