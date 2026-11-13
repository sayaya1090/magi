package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

var personActor = event.Actor{Kind: event.ActorUser, ID: "person"}

func promptOf(sid session.SessionID, text string) command.SubmitPrompt {
	return command.SubmitPrompt{SessionID: sid, Actor: personActor, Parts: []session.Part{{Kind: session.PartText, Text: text}}}
}

// After recovery the person has to be able to SEE which calls are theirs to check, settle each one,
// and only then is the turn something a "continue" may pick up. Deferring a call keeps it theirs.
func TestRecoveryStateNamesTheCallsThePersonHasToCheck(t *testing.T) {
	ctx := context.Background()
	a, store, sid, wd := crashedLog(t, "c_write")
	a.RecoverInterrupted(ctx, wd)

	st := a.RecoveryStateOf(ctx, sid)
	if st.Kind != RecoveryNeedsCheck || st.MessageID != "m_user" || len(st.Uncertain) != 1 ||
		st.Uncertain[0].CallID != "c_write" || st.Uncertain[0].Tool != "write" {
		t.Fatalf("want needs_check naming c_write (write) after m_user, got %+v", st)
	}
	if err := a.ResolveCall(ctx, sid, "c_write", VerdictDeferred, ""); err != nil {
		t.Fatal(err)
	}
	if st := a.RecoveryStateOf(ctx, sid); st.Kind != RecoveryNeedsCheck || st.Uncertain[0].Verdict != VerdictDeferred {
		t.Fatalf("a deferred call is still the person's to check: %+v", st)
	}
	if err := a.ResolveCall(ctx, sid, "c_write", VerdictNotRan, "the file is unchanged"); err != nil {
		t.Fatal(err)
	}
	if st := a.RecoveryStateOf(ctx, sid); st.Kind != RecoveryResumable || len(st.Uncertain) != 0 {
		t.Fatalf("with every uncertain call settled the turn can be continued: %+v", st)
	}
	evs, _ := store.Read(ctx, sid, 0)
	if n := countType(evs, event.TypeToolResolved); n != 2 {
		t.Fatalf("want 2 tool.resolved facts, got %d: %v", n, typesOf(evs))
	}
	for _, e := range evs {
		if e.Type == event.TypeToolResolved && e.Actor.Kind != event.ActorUser {
			t.Fatalf("a verdict is the person's, written as %+v", e.Actor)
		}
	}
	if err := a.ResolveCall(ctx, sid, "c_read", VerdictRan, ""); err == nil {
		t.Fatal("c_read never started; there is nothing about it to settle")
	}
	if err := a.ResolveCall(ctx, sid, "c_write", Verdict("maybe"), ""); err == nil {
		t.Fatal("an unknown verdict must be refused")
	}
	b, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_next", Parts: []session.Part{{Kind: session.PartText, Text: "continue"}}})
	if err := a.appendFact(ctx, sid, event.TypePromptSubmitted, personActor, b); err != nil {
		t.Fatal(err)
	}
	if st := a.RecoveryStateOf(ctx, sid); st.Kind != RecoveryNone {
		t.Fatalf("a new prompt starts a new turn; nothing is left to recover: %+v", st)
	}
}

func TestRecoveryStateOfAResumableTurnAndOfAFreshOne(t *testing.T) {
	ctx := context.Background()
	a, _, sid, wd := crashedLog(t)
	a.RecoverInterrupted(ctx, wd)
	if st := a.RecoveryStateOf(ctx, sid); st.Kind != RecoveryResumable || st.MessageID != "m_user" || len(st.Uncertain) != 0 {
		t.Fatalf("want resumable from m_user, got %+v", st)
	}
	other, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	if st := a.RecoveryStateOf(ctx, other); st.Kind != RecoveryNone {
		t.Fatalf("a session with nothing interrupted has nothing to show: %+v", st)
	}
}

// A session recovery could not settle is not the same as one with nothing to settle: it says so,
// refuses new work until recovery is retried, and leaves every other session usable.
func TestASessionWhoseRecoveryFailedRefusesNewWorkUntilRetried(t *testing.T) {
	ctx := context.Background()
	_, store, sid, wd := crashedLog(t, "c_write")
	rs := &refusingStore{Store: store, refuse: byRecovery(event.TypePartAppended)}
	a := closeAfter(t, New(rs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	a.RecoverInterrupted(ctx, wd)

	st := a.RecoveryStateOf(ctx, sid)
	if st.Kind != RecoveryFailed || !strings.Contains(st.Reason, "injected") {
		t.Fatalf("want recovery_failed with the cause, got %+v", st)
	}
	if err := a.Submit(ctx, promptOf(sid, "go on")); err == nil {
		t.Fatal("new work in a session whose recovery failed must be refused")
	}
	other, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	if st := a.RecoveryStateOf(ctx, other); st.Kind != RecoveryNone {
		t.Fatalf("another session is not affected: %+v", st)
	}

	rs.allow()
	if r := a.RetryRecovery(ctx, sid); r.Err != nil {
		t.Fatalf("retry should settle it: %+v", r)
	}
	if st := a.RecoveryStateOf(ctx, sid); st.Kind != RecoveryNeedsCheck {
		t.Fatalf("after a successful retry the call is the person's to check: %+v", st)
	}
	if err := a.Submit(ctx, promptOf(sid, "go on")); err != nil {
		t.Fatalf("after the retry the session takes work again: %v", err)
	}
}

type unlistableStore struct{ refusingStore }

func (u *unlistableStore) ListSessions(context.Context, string) ([]session.SessionMeta, error) {
	return nil, errInjected
}

// "Nothing to recover" and "could not look" must not read alike.
func TestRecoveryThatCannotListSessionsSaysItCouldNotLook(t *testing.T) {
	ctx := context.Background()
	_, store, sid, wd := crashedLog(t, "c_write")
	us := &unlistableStore{refusingStore{Store: store}}
	a := closeAfter(t, New(us, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].SessionID != "" || !errors.Is(got[0].Err, errInjected) {
		t.Fatalf("want one workspace-level failure, got %+v", got)
	}
	if st := a.RecoveryStateOf(ctx, sid); st.Kind != RecoveryFailed || st.Reason == "" {
		t.Fatalf("an unchecked workspace must not read as nothing to recover: %+v", st)
	}
}
