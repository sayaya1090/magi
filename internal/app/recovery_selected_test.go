package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

type countedRecoveryStore struct {
	*jsonl.Store
	reads        map[session.SessionID]int
	inventories  int
	emptyAppends int
}

func (s *countedRecoveryStore) Append(ctx context.Context, sid session.SessionID, evs ...event.Event) ([]int64, error) {
	if sid == "" {
		s.emptyAppends++
	}
	return s.Store.Append(ctx, sid, evs...)
}
func (s *countedRecoveryStore) Read(ctx context.Context, sid session.SessionID, seq int64) ([]event.Event, error) {
	s.reads[sid]++
	return s.Store.Read(ctx, sid, seq)
}
func (s *countedRecoveryStore) AllSessions(ctx context.Context, wd string) ([]session.SessionMeta, error) {
	s.inventories++
	return s.Store.AllSessions(ctx, wd)
}
func (s *countedRecoveryStore) ChildSessions(context.Context, string, string) ([]session.SessionMeta, error) {
	panic("recovery must not rescan project for every parent")
}
func TestSelectedRecoveryLeavesUnopenedConversationUntouched(t *testing.T) {
	ctx := context.Background()
	seed, store, first, wd := crashedLog(t)
	second, err := seed.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if err = seed.appendPromptText(ctx, second, crashActor, "another task"); err != nil {
		t.Fatal(err)
	}
	child := recoveryChild(t, seed, first, wd)
	counted := &countedRecoveryStore{Store: store, reads: map[session.SessionID]int{}}
	a := closeAfter(t, New(counted, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{}))
	got := a.RecoverSelected(ctx, wd, first)
	if len(got) != 2 || got[0].SessionID != child || got[1].SessionID != first {
		t.Fatalf("selected tree: %+v", got)
	}
	if counted.reads[second] != 0 {
		t.Fatal("read unopened conversation")
	}
	if counted.inventories != 1 {
		t.Fatalf("inventory scans=%d", counted.inventories)
	}
	evs, _ := store.Read(ctx, second, 0)
	if _, open := unfinishedTurn(evs); !open {
		t.Fatal("unopened conversation was changed")
	}
	_, cancel, err := a.Subscribe(ctx, second, 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	evs, _ = store.Read(ctx, second, 0)
	if _, open := unfinishedTurn(evs); open {
		t.Fatal("opening conversation did not recover it")
	}
	scans := counted.inventories
	_, cancel, err = a.Subscribe(ctx, second, 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if counted.inventories != scans {
		t.Fatal("reattaching repeats recovery")
	}
}

func TestStartupModelChangeDoesNotAppendToEmptySession(t *testing.T) {
	_, store, _, _ := crashedLog(t)
	counted := &countedRecoveryStore{Store: store, reads: map[session.SessionID]int{}}
	a := closeAfter(t, New(counted, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{}))
	a.SetModel("", "startup-model")
	if counted.emptyAppends != 0 {
		t.Fatal("startup tried to persist an empty session")
	}
	evs, err := store.Read(context.Background(), "", 0)
	if err != nil || len(evs) != 0 {
		t.Fatalf("empty session log: %v %v", evs, err)
	}
}
