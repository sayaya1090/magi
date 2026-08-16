package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func continueDelib(feedback string) council.Deliberation {
	return council.Deliberation{Round: 1, Decision: council.Continue, Feedback: feedback,
		Verdicts: []council.Verdict{{Member: "Balthasar", Lens: "verification",
			Decision: council.Continue, Feedback: feedback}}}
}

// The rejection cap: a stretch of rejected declarations with NOTHING changing between them lands
// the turn UNVERIFIED instead of cycling declare→reject forever. Measured live before this existed
// (the deny-mode and headless-ask wave tests): a task made impossible by the run's own permission
// mode was declared honestly and rejected eighteen consecutive rounds over forty-six minutes,
// until an external kill — while the manual promised an honest failure is a terminal outcome.
func TestRejectionCapLandsAStuckTurnUnverified(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{continueDelib("the command never ran")}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)

	// Two rejections at the same epoch: still the council's ordinary "not yet".
	for i := 0; i < 2; i++ {
		out, err := a.councilAdvice(ctx, s, nil, 7, "", true)
		if err != nil {
			t.Fatalf("declaration %d: %v", i+1, err)
		}
		if !strings.Contains(out, "does NOT accept") {
			t.Fatalf("rejection %d should be the ordinary continue:\n%s", i+1, out)
		}
		if a.takeTurnControl(sid).finish {
			t.Fatalf("rejection %d must not end the turn", i+1)
		}
	}
	// The third with no mutation in between trips the cap.
	out, err := a.councilAdvice(ctx, s, nil, 7, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "UNVERIFIED") || !strings.Contains(out, "honest account") {
		t.Errorf("the cap's landing should say what is happening and ask for the honest account:\n%s", out)
	}
	ctrl := a.takeTurnControl(sid)
	if !ctrl.finish {
		t.Fatal("the cap must signal the loop to land the turn")
	}
	if !strings.Contains(ctrl.unverifiedReason, "rejected 3 completion declarations") {
		t.Errorf("the landing must carry the reason for the record, got %q", ctrl.unverifiedReason)
	}
}

// Real work between rejections is the gate WORKING — iterate, fix, declare again. The short cap
// only counts rejections with no mutation between them, so an epoch that moves resets it.
func TestRejectionCapGivesRopeToRealIteration(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{continueDelib("still failing")}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)

	// Four rejections, each after new work (a different epoch, and a different report every
	// time — the identical-redeclaration short-circuit is for an agent that changed NOTHING,
	// which is not this one): no landing.
	actor := event.Actor{Kind: event.ActorAgent, ID: "default"}
	for epoch := 1; epoch <= 4; epoch++ {
		a.appendPart(ctx, sid, actor, "m_it", session.RoleAssistant, session.Part{
			Kind: session.PartText, Text: fmt.Sprintf("attempt %d: reworked the fix", epoch)})
		out, err := a.councilAdvice(ctx, s, nil, epoch, "", true)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "does NOT accept") {
			t.Fatalf("epoch %d: iteration with real work must stay the ordinary continue:\n%s", epoch, out)
		}
		if a.takeTurnControl(sid).finish {
			t.Fatalf("epoch %d: the cap fired on healthy iteration", epoch)
		}
	}

	// An acceptance resets the counters entirely.
	fc.mu.Lock()
	fc.delibs = []council.Deliberation{{Round: 1, Decision: council.Done,
		Verdicts: []council.Verdict{{Member: "Balthasar", Decision: council.Done}}}}
	fc.calls = len(fc.delibs) // past the script → repeats the last (done)
	fc.mu.Unlock()
	if _, err := a.councilAdvice(ctx, s, nil, 5, "", true); err != nil {
		t.Fatal(err)
	}
	if !a.takeTurnControl(sid).finish {
		t.Fatal("the accepted declaration should still land the turn")
	}
}
