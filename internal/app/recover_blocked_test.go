package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// childListFails cannot list the children of one session.
type childListFails struct {
	refusingStore
	bad session.SessionID
}

func (c *childListFails) ChildSessions(ctx context.Context, wd, parent string) ([]session.SessionMeta, error) {
	if session.SessionID(parent) == c.bad {
		return nil, errInjected
	}
	return c.refusingStore.Store.ChildSessions(ctx, wd, parent)
}

func anyErrIs(got []RecoveredTurn, target error) bool {
	for _, r := range got {
		if errors.Is(r.Err, target) {
			return true
		}
	}
	return false
}

// A parent whose turn is already closed has nothing of its own left to settle. If its children
// cannot be listed that is reported, but the parent is not marked failed: it is the CHILDREN that
// went unchecked, and a closed conversation must keep taking work.
func TestAClosedParentIsNotBlockedWhenItsChildrenCannotBeListed(t *testing.T) {
	ctx := context.Background()
	_, store, parent, wd := crashedLog(t)
	seed := closeAfter(t, New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	recoveryChild(t, seed, parent, wd)
	seed.RecoverInterrupted(ctx, wd)
	if again := seed.RecoverInterrupted(ctx, wd); len(again) != 0 {
		t.Fatalf("precondition: everything settled, got %+v", again)
	}

	cs := &childListFails{refusingStore: refusingStore{Store: store}, bad: parent}
	a := closeAfter(t, New(cs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	got := a.RecoverInterrupted(ctx, wd)
	if !anyErrIs(got, errInjected) {
		t.Fatalf("the unlisted children must still be reported, got %+v", got)
	}
	for _, r := range got {
		if r.SessionID == parent && r.Err != nil && !r.ChildrenUnchecked {
			t.Fatalf("the closed parent is reported as its own failure: %+v", r)
		}
	}
	if st := a.RecoveryStateOf(ctx, parent); st.Kind == RecoveryFailed {
		t.Fatalf("a closed parent must not read as recovery_failed: %+v", st)
	}
	if err := a.Submit(ctx, promptOf(parent, "next")); err != nil {
		t.Fatalf("a closed parent must keep taking work: %v", err)
	}
}

// A child from an EARLIER turn whose recovery cannot be saved fails on its own. The parent has
// since asked for and finished other work, and is not held back by it.
func TestAnOldChildFailureStaysWithTheChild(t *testing.T) {
	ctx := context.Background()
	_, store, parent, wd := crashedLog(t)
	seed := closeAfter(t, New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	child := recoveryChild(t, seed, parent, wd)
	if err := seed.finishRecovered(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := seed.appendPromptText(ctx, parent, crashActor, "new task"); err != nil {
		t.Fatal(err)
	}
	if err := seed.finishRecovered(ctx, parent); err != nil {
		t.Fatal(err)
	}

	rs := &refusingStore{Store: store, refuse: func(e event.Event) bool { return e.SessionID == child }}
	a := closeAfter(t, New(rs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	got := a.RecoverInterrupted(ctx, wd)
	childFailed := false
	for _, r := range got {
		if r.SessionID == child && errors.Is(r.Err, errInjected) {
			childFailed = true
		}
		if r.SessionID == parent && r.Err != nil {
			t.Fatalf("an old child failure was charged to the parent: %+v", got)
		}
	}
	if !childFailed {
		t.Fatalf("the child failure must be reported: %+v", got)
	}
	if st := a.RecoveryStateOf(ctx, child); st.Kind != RecoveryFailed {
		t.Fatalf("the child reads as recovery_failed: %+v", st)
	}
	if st := a.RecoveryStateOf(ctx, parent); st.Kind == RecoveryFailed {
		t.Fatalf("the parent is not: %+v", st)
	}
}

// The parent was interrupted again in a NEWER turn, and a child of its earlier turn cannot be
// saved. The newer turn is settled on its own: only children created since its prompt can hold it.
func TestAnOldChildFailureDoesNotHoldTheParentsOpenTurn(t *testing.T) {
	ctx := context.Background()
	_, store, parent, wd := crashedLog(t)
	seed := closeAfter(t, New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	child := recoveryChild(t, seed, parent, wd)
	if err := seed.finishRecovered(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := seed.appendPromptText(ctx, parent, crashActor, "new task"); err != nil {
		t.Fatal(err)
	}

	rs := &refusingStore{Store: store, refuse: func(e event.Event) bool { return e.SessionID == child }}
	a := closeAfter(t, New(rs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	got := a.RecoverInterrupted(ctx, wd)
	settled := false
	for _, r := range got {
		if r.SessionID == parent {
			if r.Err != nil {
				t.Fatalf("an old child failure held back the parents new turn: %+v", got)
			}
			settled = true
		}
	}
	if !settled {
		t.Fatalf("the parents open turn must be settled: %+v", got)
	}
	if !anyErrIs(got, errInjected) {
		t.Fatalf("the child failure must still be reported: %+v", got)
	}
	if st := a.RecoveryStateOf(ctx, parent); st.Kind == RecoveryFailed {
		t.Fatalf("the parent is not recovery_failed: %+v", st)
	}
}
