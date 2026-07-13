package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// appendToolCall writes a tool-call part into the session log — "the agent DID
// something" for the no-progress delta gate's fingerprint.
func appendToolCall(t *testing.T, a *App, sid session.SessionID, name string) {
	t.Helper()
	d, _ := json.Marshal(event.PartAppendedData{
		MessageID: "m_" + newID(), Role: session.RoleAssistant,
		Part: session.Part{ID: "p_" + newID(), Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: "c_" + newID(), Name: name, Args: []byte(`{}`)}},
	})
	if err := a.appendFact(context.Background(), sid, event.TypePartAppended,
		event.Actor{Kind: event.ActorAgent, ID: "default"}, d); err != nil {
		t.Fatal(err)
	}
}

// A re-finish with ZERO new tool actions since the last rejection reuses the
// standing verdict without deliberating; the reused rounds still count, so the
// round cap lands (UNVERIFIED) without further council cost.
func TestCouncilNoProgressDeltaGateSkipsDeliberation(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "run the real test", Verdicts: []council.Verdict{
			{Member: "Melchior", Decision: council.Continue, Feedback: "run the real test"},
			{Member: "Balthasar", Decision: council.Done},
			{Member: "Casper", Decision: council.Done},
		}},
	}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc, CouncilMaxRounds: 3})
	s, agent := newGateSession(t, a, wd)
	ctx := context.Background()
	in := councilInput{turnTask: "task", lastText: "done, trust me", stepsLeft: 40}

	var ct councilTurn
	if keep, _ := a.runCouncilGate(ctx, s, agent, in, &ct); !keep {
		t.Fatal("round 1 rejection should continue")
	}
	if fc.calls != 1 {
		t.Fatalf("round 1 deliberates once, calls=%d", fc.calls)
	}

	// Re-finish with no new tool activity → verdict reused, NO deliberation.
	if keep, _ := a.runCouncilGate(ctx, s, agent, in, &ct); !keep {
		t.Fatal("reused rejection should still continue")
	}
	if fc.calls != 1 {
		t.Fatalf("no-progress re-finish must not deliberate, calls=%d", fc.calls)
	}
	if ct.rounds != 2 {
		t.Fatalf("the reused rejection must consume a round, rounds=%d", ct.rounds)
	}

	// Third no-progress re-finish hits the cap → forced UNVERIFIED landing.
	keep, reason := a.runCouncilGate(ctx, s, agent, in, &ct)
	if keep || !strings.Contains(reason, "never approved") {
		t.Fatalf("cap on a no-progress loop must land UNVERIFIED, keep=%v reason=%q", keep, reason)
	}
	if fc.calls != 1 {
		t.Fatalf("the whole loop should have deliberated exactly once, calls=%d", fc.calls)
	}
	if !ct.deadlocked {
		t.Fatal("cap landing is a genuine deadlock")
	}
}

// A re-finish WITH new actions re-polls only the dissenting member: prior done
// votes are carried, the request is a delta round focused on the standing concern.
func TestCouncilFocusedReRound(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "server is not proven running", Verdicts: []council.Verdict{
			{Member: "Melchior", Decision: council.Continue, Feedback: "server is not proven running"},
			{Member: "Balthasar", Decision: council.Done},
			{Member: "Casper", Decision: council.Done},
		}},
		{Round: 2, Decision: council.Done},
	}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc, CouncilMaxRounds: 3})
	s, agent := newGateSession(t, a, wd)
	ctx := context.Background()
	in := councilInput{turnTask: "task", lastText: "report", stepsLeft: 40}

	var ct councilTurn
	if keep, _ := a.runCouncilGate(ctx, s, agent, in, &ct); !keep {
		t.Fatal("round 1 rejection should continue")
	}
	appendToolCall(t, a, s.ID, "bash") // real new action → not the no-progress path

	if keep, _ := a.runCouncilGate(ctx, s, agent, in, &ct); keep {
		t.Fatal("round 2 done vote should finish")
	}
	if fc.calls != 2 {
		t.Fatalf("round 2 must deliberate, calls=%d", fc.calls)
	}
	req := fc.lastReq
	if !req.DeltaRound {
		t.Fatal("round 2 with a prior split vote must be a delta round")
	}
	if len(req.Members) != 1 || req.Members[0].Name != "Melchior" {
		t.Fatalf("only the dissenting member re-polls, got %+v", req.Members)
	}
	if len(req.CarriedDone) != 2 {
		t.Fatalf("prior done votes must be carried, got %d", len(req.CarriedDone))
	}
	if !strings.Contains(req.PriorConcern["Melchior"], "not proven running") {
		t.Fatalf("the member's standing concern must ride the request: %+v", req.PriorConcern)
	}
}
