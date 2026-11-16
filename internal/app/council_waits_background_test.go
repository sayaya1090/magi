package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The council judges results. While background work this conversation started is still running the
// results do not exist yet, so a declaration is not judged: the turn ends waiting, and the note the
// finished job brings back (background_wake.go) opens the turn where the agent declares and the
// council convenes (2026-11-17: "the council should wait until the wait ends" — it rejected a
// declaration for missing results and held the agent in a turn the person could not reach).
func TestTheCouncilWaitsForRunningBackgroundWork(t *testing.T) {
	ctx := context.Background()
	spy := &spyCouncil{}
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow", Council: spy})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err := a.appendPrompt(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "build it"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	a.appendPart(ctx, sid, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "m1", session.RoleAssistant,
		session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c_bg", Name: "bash"}})
	a.backgroundMu.Lock()
	if a.backgroundOwned == nil {
		a.backgroundOwned = map[string]bool{}
	}
	a.backgroundOwned[string(sid)+"/c_bg"] = true
	a.backgroundMu.Unlock()
	s := a.sessionInfo(ctx, sid)

	out, err := a.councilToolAdvice(ctx, s, nil, 0, "", true, true)
	if err != nil || spy.called {
		t.Fatalf("the council convened over work still running: called=%v err=%v", spy.called, err)
	}
	if !strings.Contains(out, "c_bg") || !strings.Contains(out, "background job finished") {
		t.Errorf("the agent must be told what is running and where to declare: %s", out)
	}
	control := a.takeTurnControl(sid)
	if !control.finish || !strings.Contains(control.unverifiedReason, "background") {
		t.Errorf("the turn must end waiting on the background work: %+v", control)
	}

	// Going quiet instead of declaring: no reminder, the turn ends waiting all the same.
	tc := turnCtx{s: s, agent: AgentSpec{Name: "coder"}, guard: newRunGuard(nil)}
	ts := &turnState{}
	if _, done := a.requireFinishDeclaration(ctx, tc, true, "", ts); done {
		t.Error("the agent was pressed to declare while its background work runs")
	}
	if !strings.Contains(ts.unverifiedReason, "background") {
		t.Errorf("the quiet turn must end waiting on the background work: %q", ts.unverifiedReason)
	}

	// The job finished: the next declaration is judged as always.
	a.backgroundMu.Lock()
	delete(a.backgroundOwned, string(sid)+"/c_bg")
	a.backgroundMu.Unlock()
	if _, err := a.councilToolAdvice(ctx, s, nil, 0, "", true, true); err != nil || !spy.called {
		t.Fatalf("with nothing running the council must convene: called=%v err=%v", spy.called, err)
	}
}
