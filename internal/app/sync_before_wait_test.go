package app

import (
	"context"
	"sync/atomic"
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

// syncCounter is a store that counts the forced syncs asked of it.
type syncCounter struct {
	port.Store
	n atomic.Int32
}

func (s *syncCounter) SyncLog(context.Context, session.SessionID) error { s.n.Add(1); return nil }

// The log is synced to the disk at turn boundaries, not on every event (jsonl.durableBoundary). A
// person may take minutes to answer, so the record up to the question is forced down BEFORE magi
// starts waiting: a power cut while the prompt is up then loses nothing the person was shown.
func TestTheLogIsSyncedBeforeWaitingOnAPerson(t *testing.T) {
	inner, _ := jsonl.New(t.TempDir())
	store := &syncCounter{Store: inner}
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideCall("bash", `{"command":"true"}`), asideText("done")}}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil,
		Config{Permission: "ask", Interactive: true, AnswerWait: 100 * time.Millisecond}))
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "run it"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	// Wait until the prompt is actually up, then the sync must already have happened.
	asked := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && !asked; time.Sleep(5 * time.Millisecond) {
		a.mu.Lock()
		asked = len(a.stateLocked(sid).asking) > 0
		a.mu.Unlock()
	}
	if !asked {
		t.Fatal("the permission prompt never came up — the harness, not the behaviour, failed")
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && store.n.Load() == 0; time.Sleep(5 * time.Millisecond) {
	}
	if store.n.Load() == 0 {
		t.Fatal("magi waited on the permission prompt without syncing the log first")
	}
}
