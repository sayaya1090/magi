package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A council ACCEPTANCE sheds a stale cap reason. The cap lands a rejection with its reason; a
// hand_off reopens the turn; the re-judged declaration is accepted — and the acceptance signal
// carries no reason, which used to leave the old one in place: an accepted turn finishing as
// turn.finished{Unverified:true} under the rejection's words.
func TestAnAcceptedFinishShedsTheStaleCapReason(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: &fakeCouncil{}})
	ts := &turnState{unverifiedReason: "the council rejected 3 declarations"}
	a.signalTurnControl(sid, func(tc *turnControl) { tc.finish = true }) // an acceptance: finish, no reason
	if !a.finishDeclared(ts, sid) {
		t.Fatal("the acceptance signal was not adopted")
	}
	if ts.unverifiedReason != "" {
		t.Fatalf("an accepted finish still carries the stale cap reason %q", ts.unverifiedReason)
	}
}

// Reopening a declared turn (hand_off re-ask) abandons the cap's reason along with the
// declaration it belonged to.
func TestAReopenedDeclarationAbandonsTheCapReason(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: &fakeCouncil{}})
	ts := &turnState{declared: true, unverifiedReason: "capped"}
	kept := a.callsAfterDeclaring(context.Background(), sid, []*session.ToolCall{{CallID: "c1", Name: "hand_off"}}, ts)
	if len(kept) != 1 || kept[0].Name != "hand_off" {
		t.Fatalf("the re-ask must run, got %v", kept)
	}
	if ts.declared || ts.unverifiedReason != "" {
		t.Fatalf("reopening must abandon the old declaration wholesale: declared=%v reason=%q", ts.declared, ts.unverifiedReason)
	}
}

// A finish signal left undrained by a dying turn (interrupt returns from the step head before any
// drain) must not greet the next turn: with the leak, the new turn's first gate adopted the stale
// finish and dropped every tool call as "already declared" before the task did anything.
func TestTurnControlDoesNotLeakIntoTheNextTurn(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: &fakeCouncil{}})
	a.signalTurnControl(sid, func(tc *turnControl) { tc.finish = true })
	a.resetForNewTopLevel(sid)
	ts := &turnState{}
	if a.finishDeclared(ts, sid) {
		t.Fatal("the previous turn's finish signal leaked into a fresh turn")
	}
}

// The readings count is TURN-scoped: councils from before the last persisted turn.finished are an
// earlier turn's and must not inflate this turn's banner.
func TestCouncilReadingsCountOnlyThisTurn(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: &fakeCouncil{}})
	ctx := context.Background()
	actor := event.Actor{Kind: event.ActorSystem, ID: "test"}
	for _, ty := range []event.Type{event.TypeCouncilConvened, event.TypeCouncilConvened, event.TypeTurnFinished, event.TypeCouncilConvened} {
		if err := a.appendFact(ctx, sid, ty, actor, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.councilReadingsThisTurn(ctx, sid); got != 1 {
		t.Fatalf("councilReadingsThisTurn = %d, want 1 (two earlier readings belong to the finished turn)", got)
	}
}

// The banner tells the truth about a rejected declaration: "never declared" is refuted by the
// events whenever a declaration convened the gate and was rejected.
func TestUndeclaredReasonNamesARejectedDeclaration(t *testing.T) {
	if s := undeclaredReason(0, 0); !strings.Contains(s, "never declared") {
		t.Errorf("no declaration, no rejection: %q", s)
	}
	if s := undeclaredReason(2, 0); !strings.Contains(s, "2 councils") {
		t.Errorf("readings must be carried: %q", s)
	}
	s := undeclaredReason(1, 1)
	if strings.Contains(s, "never declared") || !strings.Contains(s, "rejected it once") {
		t.Errorf("a rejected declaration must not read as never-declared: %q", s)
	}
	if s := undeclaredReason(0, 3); !strings.Contains(s, "3 times") {
		t.Errorf("repeat rejections must be counted: %q", s)
	}
}

// Stop stops. The restart-after-press gate asked a different question than the press's own
// sweep: the sweep holds prompts that were OPEN at the press, while the gate listed every
// non-abandoned user prompt — answered ones included — so any session past its first turn always
// had one the sweep lacked, "fresh" was always true, and every interrupt restarted the run.
func TestAPressedStopDoesNotRestartOnAnAlreadyAnsweredPrompt(t *testing.T) {
	mk := func(seq int64, typ event.Type, kind event.ActorKind, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Seq: seq, Type: typ, Actor: event.Actor{Kind: kind}, Data: b}
	}
	evs := []event.Event{
		mk(1, event.TypePromptSubmitted, event.ActorUser, event.PromptSubmittedData{MessageID: "u1"}),
		mk(2, event.TypePartAppended, event.ActorAgent, event.PartAppendedData{MessageID: "a1",
			Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "answered"}}),
		mk(3, event.TypePromptSubmitted, event.ActorUser, event.PromptSubmittedData{MessageID: "u2"}),
	}
	// The press: u2 is open, u1 was answered long ago.
	pressed := map[string]bool{}
	for _, id := range unansweredUserPromptIDs(evs) {
		pressed[id] = true
	}
	if !pressed["u2"] || pressed["u1"] {
		t.Fatalf("the sweep must hold exactly the open prompt, got %v", pressed)
	}
	fresh := false
	for _, id := range unansweredUserPromptIDs(evs) {
		if !pressed[id] {
			fresh = true
		}
	}
	if fresh {
		t.Fatal("nothing was said after the press, yet the run would restart")
	}
	// And a request that DOES arrive after the press still runs.
	evs = append(evs, mk(4, event.TypePromptSubmitted, event.ActorUser, event.PromptSubmittedData{MessageID: "u3"}))
	fresh = false
	for _, id := range unansweredUserPromptIDs(evs) {
		if !pressed[id] {
			fresh = true
		}
	}
	if !fresh {
		t.Fatal("a request made after the press must still run")
	}
}
