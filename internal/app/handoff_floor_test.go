package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// handedSession opens a conversation the way a handover does, and an ordinary one beside it.
func handedSession(t *testing.T, cfg Config) (*App, session.SessionID, session.SessionID) {
	t.Helper()
	a, mine, wd := newWorkflowApp(t, nil, &scriptPlatform{}, cfg)
	theirs, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: wd,
		Actor:   event.Actor{Kind: event.ActorUser, ID: HandoffActorID("design")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, mine, theirs
}

// Work another companion asked for is not covered by this operator's standing yes.
//
// The approval mode says what the person wants done on their behalf without being asked. A
// handed-over turn is not on their behalf: another agent chose what to ask for, its words arrive
// verbatim, and the mode was set with this operator's own work in mind. So `allow` and `auto` do
// not carry into one — and with nobody there to ask, it is refused rather than resolved by policy,
// which is precisely the four-in-the-morning case the floor exists for.
func TestAHandedOverTurnDoesNotInheritTheStandingYes(t *testing.T) {
	// The tool each mode says yes to on the operator's own turn: everything under `allow`, and
	// edits under `auto`. Those are the two paths a handed-over turn must not take.
	for _, tc := range []struct{ mode, tool string }{{"allow", "bash"}, {"auto", "write"}} {
		// Not interactive: no console, no terminal — the state a daemon does this work in.
		a, mine, theirs := handedSession(t, Config{Permission: tc.mode})
		call := &session.ToolCall{CallID: "c1", Name: tc.tool}
		if !a.requestPermission(context.Background(), mine, event.Actor{}, call, false, "") {
			t.Errorf("%s: the operator's own %s was refused, which is not what this changes",
				tc.mode, tc.tool)
		}
		if a.requestPermission(context.Background(), theirs, event.Actor{}, call, false, "") {
			t.Errorf("%s: another companion's request ran %s with nobody to ask", tc.mode, tc.tool)
		}
	}
}

// And `deny` still denies, in both — the floor only ever takes away.
func TestTheFloorNeverGrantsWhatTheModeRefuses(t *testing.T) {
	a, _, theirs := handedSession(t, Config{Permission: "deny"})
	call := &session.ToolCall{CallID: "c1", Name: "bash"}
	if a.requestPermission(context.Background(), theirs, event.Actor{}, call, false, "") {
		t.Error("a denied console approved a handed-over call")
	}
}

// A person may still say yes — once. A standing grant in a conversation that belongs to one asker
// would outlive the request that earned it, so the next call asks again, and the transcript says
// that is what happened rather than leaving it to look like a bug.
func TestAlwaysInAHandedOverTurnIsThisCallOnly(t *testing.T) {
	a, _, theirs := handedSession(t, Config{Permission: "ask", Interactive: true})
	call := &session.ToolCall{CallID: "c1", Name: "bash"}

	answer := func(id, dec string) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if a.RespondPermission(context.Background(), command.RespondPermission{
				SessionID: theirs, CallID: id, Decision: dec}) == nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Errorf("nothing was waiting for %s", id)
	}
	go answer("c1", "always")
	if !a.requestPermission(context.Background(), theirs, event.Actor{}, call, false, "") {
		t.Fatal("the person said yes and the call was refused")
	}
	// The second call must ask again rather than ride the grant.
	next := &session.ToolCall{CallID: "c2", Name: "bash"}
	go answer("c2", "deny")
	if a.requestPermission(context.Background(), theirs, event.Actor{}, next, false, "") {
		t.Error("the grant carried to the next call, so one yes approved everything after it")
	}
}
