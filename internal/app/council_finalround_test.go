package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// A FINAL-round rejection must land the turn immediately (forced UNVERIFIED),
// not inject feedback: injected feedback opens one more unbounded work phase
// that the pre-round cap can only close at the NEXT no-tool-call finish attempt,
// which a completion-looping model never produces (it burns the wall clock and
// the timeout kill takes running deliverable processes down with it).
func TestCouncilGateFinalRoundRejectionLandsImmediately(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "still unmet A"},
		{Round: 2, Decision: council.Continue, Feedback: "still unmet B"},
	}}
	a, wd := newApp(t, workingLLM(), Config{Council: fc, CouncilMaxRounds: 2})
	s, agent := newGateSession(t, a, wd)
	ctx := context.Background()
	in := councilInput{turnTask: "task", lastText: "report", stepsLeft: 40}

	var ct councilTurn
	keep, _ := a.runCouncilGate(ctx, s, agent, in, &ct)
	if !keep {
		t.Fatal("round 1 rejection (below the cap) must inject feedback and continue")
	}

	keep, reason := a.runCouncilGate(ctx, s, agent, in, &ct)
	if keep {
		t.Fatal("the final allowed round's rejection must finish the turn, not continue")
	}
	if reason == "" || !strings.Contains(reason, "never approved") {
		t.Fatalf("finish must carry the unverified reason, got %q", reason)
	}
	if !ct.deadlocked {
		t.Error("a final-round rejection is a genuine deadlock (hook B must be able to fire)")
	}
	if fc.calls != 2 {
		t.Errorf("both rounds must actually deliberate; calls=%d", fc.calls)
	}

	// Exactly ONE feedback injection (round 1). The final round records a forced
	// UNVERIFIED decision instead of prompting more work.
	evs := mustRead(t, a, s.ID)
	injected := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "council" {
			injected++
		}
	}
	if injected != 1 {
		t.Errorf("council feedback prompts = %d, want 1 (no injection on the landing round)", injected)
	}
	unverified := false
	for _, e := range evs {
		if e.Type == event.TypeCouncilDecided && strings.Contains(string(e.Data), "UNVERIFIED") {
			unverified = true
		}
	}
	if !unverified {
		t.Error("the landing must be recorded as a forced UNVERIFIED decision")
	}
}
