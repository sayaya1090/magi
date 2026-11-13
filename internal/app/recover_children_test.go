package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func recoveryChild(t *testing.T, a *App, parent session.SessionID, project string) session.SessionID {
	t.Helper()
	ctx := context.Background()
	id, err := a.CreateSession(ctx, command.CreateSession{Parent: string(parent), Project: project, Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.appendPromptText(ctx, id, crashActor, "child task"); err != nil {
		t.Fatal(err)
	}
	a.appendPart(ctx, id, crashActor, "m_child", session.RoleAssistant, session.Part{ID: "p_call", Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "child_write", Name: "write", Args: json.RawMessage(`{}`)}})
	b, _ := json.Marshal(event.ToolStartedData{CallID: "child_write", Tool: "write"})
	if err := a.appendFact(ctx, id, event.TypeToolStarted, crashActor, b); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRecoverySettlesClonedChildrenBeforeParent(t *testing.T) {
	ctx := context.Background()
	a, store, parent, wd := crashedLog(t)
	child := recoveryChild(t, a, parent, wd)
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 2 || got[0].SessionID != child || got[1].SessionID != parent || got[1].Resumable || len(got[1].UncertainChildren) != 1 {
		t.Fatalf("child then uncertain parent: %+v", got)
	}
	evs, _ := store.Read(ctx, parent, 0)
	result := resultsByCall(t, evs)["c_write"]
	if !strings.Contains(string(result.Content), string(child)) {
		t.Fatalf("parent lost child reference: %s", result.Content)
	}
	if _, open := a.UnfinishedTurnOf(ctx, child); open {
		t.Fatal("child still open")
	}
	if again := a.RecoverInterrupted(ctx, wd); len(again) != 0 {
		t.Fatalf("not idempotent: %+v", again)
	}
}

func TestRecoveryChildFailureLeavesParentOpenAndRetryKeepsUncertainty(t *testing.T) {
	ctx := context.Background()
	a, store, parent, wd := crashedLog(t)
	child := recoveryChild(t, a, parent, wd)
	rs := &refusingStore{Store: a.store, refuse: func(e event.Event) bool { return e.SessionID == child && byRecovery(event.TypePartAppended)(e) }}
	a.store = rs
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 2 || got[0].Err == nil || got[1].Err == nil {
		t.Fatalf("failure must reach parent: %+v", got)
	}
	evs, _ := store.Read(ctx, parent, 0)
	if countByRecovery(evs, event.TypeTurnFinished) != 0 {
		t.Fatal("parent closed before child")
	}
	rs.refuse = func(e event.Event) bool { return e.SessionID == parent && byRecovery(event.TypeError)(e) }
	a.RecoverInterrupted(ctx, wd) // child now closed, parent result saved, ending refused
	rs.allow()
	got = a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].SessionID != parent || got[0].Resumable || len(got[0].UncertainChildren) != 1 {
		t.Fatalf("saved child uncertainty lost on retry: %+v", got)
	}
}

func TestRecoveryVisitsChildrenOfFinishedParents(t *testing.T) {
	ctx := context.Background()
	a, _, parent, wd := crashedLog(t)
	child := recoveryChild(t, a, parent, wd)
	if err := a.finishRecovered(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := a.appendFact(ctx, child, event.TypeError, crashActor, json.RawMessage(`{"message":"provider failed"}`)); err != nil {
		t.Fatal(err)
	}
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].SessionID != child || got[0].Resumable {
		t.Fatalf("error-only child must finish: %+v", got)
	}
}

func TestRecoveryDoesNotTouchLiveParentsChildren(t *testing.T) {
	ctx := context.Background()
	a, store, parent, wd := crashedLog(t)
	child := recoveryChild(t, a, parent, wd)
	before, _ := store.Read(ctx, child, 0)
	a.mu.Lock()
	a.stateLocked(parent).cancel = func() {}
	a.mu.Unlock()
	a.RecoverInterrupted(ctx, wd)
	after, _ := store.Read(ctx, child, 0)
	if len(after) != len(before) {
		t.Fatal("recovery touched live parent's child")
	}
	a.mu.Lock()
	a.stateLocked(parent).cancel = nil
	a.mu.Unlock()
}

func TestRecoveryOldChildDoesNotMakeNewParentTurnUncertain(t *testing.T) {
	ctx := context.Background()
	a, _, parent, wd := crashedLog(t)
	child := recoveryChild(t, a, parent, wd)
	a.RecoverInterrupted(ctx, wd)
	if err := a.appendPromptText(ctx, parent, crashActor, "new task"); err != nil {
		t.Fatal(err)
	}
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].SessionID != parent || !got[0].Resumable || len(got[0].UncertainChildren) != 0 {
		t.Fatalf("old child %s contaminated new task: %+v", child, got)
	}
}

type recoveryChildrenRefused struct{ *refusingStore }

func (s recoveryChildrenRefused) ChildSessions(context.Context, string, string) ([]session.SessionMeta, error) {
	return nil, errInjected
}

func TestRecoveryCannotCloseParentWhenChildrenCannotBeListed(t *testing.T) {
	ctx := context.Background()
	a, store, parent, wd := crashedLog(t)
	a.store = recoveryChildrenRefused{&refusingStore{Store: a.store}}
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].Err == nil {
		t.Fatalf("child listing error hidden: %+v", got)
	}
	evs, _ := store.Read(ctx, parent, 0)
	if countByRecovery(evs, event.TypeTurnFinished) != 0 {
		t.Fatal("closed without checking children")
	}
}

func TestRecoveryRechecksChildrenAfterParentInterruptionWasSaved(t *testing.T) {
	ctx := context.Background()
	a, store, parent, wd := crashedLog(t)
	a.RecoverInterrupted(ctx, wd)
	if err := a.appendPromptText(ctx, parent, crashActor, "next task"); err != nil {
		t.Fatal(err)
	}
	child := recoveryChild(t, a, parent, wd)
	// Shutdown saved a resumable interruption, but died before the terminal
	// event. A child was still writing when the process went away.
	b, _ := json.Marshal(event.ErrorData{Code: "interrupted", Resumable: true, Message: "shutdown"})
	if err := a.appendFact(ctx, parent, event.TypeError, crashActor, b); err != nil {
		t.Fatal(err)
	}
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 2 || got[1].SessionID != parent || got[1].Resumable || len(got[1].UncertainChildren) != 1 || got[1].UncertainChildren[0] != child {
		t.Fatalf("pending parent ending lost child uncertainty: %+v", got)
	}
	evs, _ := store.Read(ctx, parent, 0)
	var last event.ErrorData
	for _, e := range evs {
		if e.Type == event.TypeError {
			last = event.ErrorData{}
			_ = json.Unmarshal(e.Data, &last)
		}
	}
	if last.Resumable || !strings.Contains(last.Message, string(child)) {
		t.Fatalf("saved parent interruption lost child uncertainty: %+v", last)
	}
	if again := a.RecoverInterrupted(ctx, wd); len(again) != 0 {
		t.Fatalf("not idempotent: %+v", again)
	}
}

func TestRecoveryPendingParentRetriesChildWarningWrites(t *testing.T) {
	for _, refused := range []event.Type{event.TypeError, event.TypeTurnFinished} {
		t.Run(string(refused), func(t *testing.T) {
			ctx := context.Background()
			a, _, parent, wd := crashedLog(t)
			a.RecoverInterrupted(ctx, wd)
			if err := a.appendPromptText(ctx, parent, crashActor, "next task"); err != nil {
				t.Fatal(err)
			}
			child := recoveryChild(t, a, parent, wd)
			b, _ := json.Marshal(event.ErrorData{Code: "interrupted", Resumable: true})
			if err := a.appendFact(ctx, parent, event.TypeError, crashActor, b); err != nil {
				t.Fatal(err)
			}
			rs := &refusingStore{Store: a.store, refuse: func(e event.Event) bool {
				return e.SessionID == parent && byRecovery(refused)(e)
			}}
			a.store = rs
			got := a.RecoverInterrupted(ctx, wd)
			if len(got) != 2 || got[1].Err == nil {
				t.Fatalf("write failure hidden: %+v", got)
			}
			rs.allow()
			got = a.RecoverInterrupted(ctx, wd)
			if len(got) != 1 || got[0].Err != nil || got[0].Resumable || len(got[0].UncertainChildren) != 1 || got[0].UncertainChildren[0] != child {
				t.Fatalf("retry lost uncertainty: %+v", got)
			}
			if again := a.RecoverInterrupted(ctx, wd); len(again) != 0 {
				t.Fatalf("not idempotent: %+v", again)
			}
		})
	}
}
