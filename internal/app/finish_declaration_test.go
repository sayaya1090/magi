package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// A turn ends because someone decided to end it. Going quiet is not a decision: a turn that trailed
// off mid-thought and a turn that was actually finished used to end identically, and neither was
// ever asked which one it was.
func TestAWorkingTurnMustDeclareItIsFinished(t *testing.T) {
	fc := &fakeCouncil{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()
	tc := turnCtx{s: a.sessionInfo(ctx, sid), agent: AgentSpec{Name: "coder"}, guard: newRunGuard()}
	ts := &turnState{}

	act, done := a.requireFinishDeclaration(ctx, tc, true, ts)
	if !done || act != loopContinue {
		t.Fatalf("a working turn that never declared completion must keep going, got act=%v done=%v", act, done)
	}
	txt := sessionText(t, a, sid)
	if !strings.Contains(txt, "complete: true") || !strings.Contains(txt, "council") {
		t.Errorf("the agent must be told exactly how to declare it:\n%s", txt)
	}

	// A conversational turn has nothing to declare, and demanding one would be a loop with no exit.
	if _, done := a.requireFinishDeclaration(ctx, tc, false, ts); done {
		t.Error("a turn that did no work must be allowed to end")
	}
	// It keeps asking while there is reason to — going quiet once is not a way around it.
	if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); !done {
		t.Error("a second silent finish must still be held")
	}
	// But bounded: an agent that cannot produce the declaration would otherwise hold the session
	// open until the wall clock, answering every reminder and finishing nothing. After the cap the
	// work lands as it stands, and the turn is recorded as ending undeclared.
	for i := 0; i < declareAskCap; i++ {
		a.requireFinishDeclaration(ctx, tc, true, ts)
	}
	if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); done {
		t.Error("the ask must be bounded — a turn that cannot declare still has to end")
	}
	if !strings.Contains(ts.unverifiedReason, "never declared") {
		t.Errorf("ending undeclared must be recorded as such, got %q", ts.unverifiedReason)
	}
	// The A/B baseline restores the passive finish.
	t.Setenv("MAGI_DECLARE_FINISH", "0")
	if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); done {
		t.Error("MAGI_DECLARE_FINISH=0 must restore the passive finish")
	}
}

// With no council there is nobody to declare completion TO, so the requirement cannot apply — it
// would hold the turn open against a door that does not exist.
func TestNoCouncilNoDeclarationRequired(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	a.cfg.Workflow = false
	ctx := context.Background()
	tc := turnCtx{s: a.sessionInfo(ctx, sid), agent: AgentSpec{Name: "coder"}, guard: newRunGuard()}
	if _, done := a.requireFinishDeclaration(ctx, tc, true, &turnState{}); done {
		t.Error("without a council the turn must be free to end")
	}
}

// The declaration is answered, not rubber-stamped: an accepting council ends the turn (the loop
// reads the signal), a rejecting one hands back what is undone and the agent keeps working.
func TestDeclaringCompletionIsAnsweredByTheCouncil(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "the test file was never run",
			Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Decision: council.Continue,
				Feedback: "the test file was never run"}}},
		{Round: 1, Decision: council.Done,
			Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Decision: council.Done,
				Feedback: "it runs and the output matches"}}},
	}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)

	out, err := a.councilAdvice(ctx, s, nil, "", true)
	if err != nil {
		t.Fatalf("declaring completion: %v", err)
	}
	if !strings.Contains(out, "does NOT accept") || !strings.Contains(out, "never run") {
		t.Errorf("a rejected declaration must say so and carry the reason:\n%s", out)
	}
	if a.takeTurnControl(sid).finish {
		t.Fatal("a rejected declaration must NOT end the turn")
	}

	out, err = a.councilAdvice(ctx, s, nil, "", true)
	if err != nil {
		t.Fatalf("second declaration: %v", err)
	}
	if !strings.Contains(out, "accepts that the task is finished") {
		t.Errorf("an accepted declaration must say so:\n%s", out)
	}
	if !a.takeTurnControl(sid).finish {
		t.Error("an accepted declaration must signal the loop to end the turn")
	}
}

// Asking for advice is not declaring completion: a council call without the flag must never end the
// turn, or every question would be a resignation.
func TestAskingAdviceDoesNotEndTheTurn(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Round: 1, Decision: council.Done,
		Verdicts: []council.Verdict{{Member: "Casper", Lens: "completeness", Decision: council.Done, Feedback: "looks fine"}},
	}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	ctx := context.Background()

	out, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "is the empty input handled?", false)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if strings.Contains(out, "turn ends") {
		t.Errorf("a question must not read as a finish:\n%s", out)
	}
	if a.takeTurnControl(sid).finish {
		t.Error("asking for advice must not end the turn")
	}
	if len(fc.reqs) == 0 || !strings.Contains(fc.reqs[0].Task, "is the empty input handled?") {
		t.Error("the agent's question must reach the members")
	}
}

// The decided FACT carries the tally even though what the agent reads deliberately does not: three
// surfaces render it — the headless transcript, the TUI verdict line, the loop map — and with it
// left zero they all printed "0 done / 0 continue" under a decision three members had voted on.
func TestAcceptedDeclarationRecordsTheTally(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Round: 1, Decision: council.Done,
		Breakdown: council.Breakdown{Done: 3, Voters: 3, Rule: council.RuleMajority},
		Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Done},
			{Member: "Balthasar", Lens: "verification", Decision: council.Done},
			{Member: "Casper", Lens: "completeness", Decision: council.Done},
		},
	}}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	ctx := context.Background()
	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "", true); err != nil {
		t.Fatalf("declaring completion: %v", err)
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evs {
		if e.Type != event.TypeCouncilDecided {
			continue
		}
		var d event.CouncilDecidedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		found = true
		if d.Tally.Done != 3 || d.Tally.Voters != 3 {
			t.Errorf("the decided fact must carry the real tally, got %+v", d.Tally)
		}
	}
	if !found {
		t.Fatal("no council.decided fact was recorded")
	}
}

// The budget is per STRETCH of no progress, not per turn.
//
// It counted for the whole turn and never reset, so three quiet moments an hour apart — with real
// work between them — spent the same budget as an agent stuck in place, and the turn ended on the
// third as though nothing had happened since the first. A long productive turn does go quiet more
// than once; that is not the failure the cap is for.
func TestWorkDoneSinceTheLastAskRestartsTheBudget(t *testing.T) {
	fc := &fakeCouncil{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()
	g := newRunGuard()
	tc := turnCtx{s: a.sessionInfo(ctx, sid), agent: AgentSpec{Name: "coder"}, guard: g}
	ts := &turnState{}

	// Quiet three times with nothing produced: the cap lands, as it must.
	for i := 0; i < declareAskCap; i++ {
		if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); !done {
			t.Fatalf("ask %d was not made", i+1)
		}
	}
	if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); done {
		t.Fatal("an agent that produced nothing must still be bounded")
	}

	// Now it writes something real. The next quiet moment is a fresh stretch, and the reminder is
	// worth making again.
	ts.unverifiedReason = ""
	if !g.mutated("/app/x.c", "one") {
		t.Fatal("the fixture did not record a mutation")
	}
	if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); !done {
		t.Error("work landed since the last ask and the agent was not reminded again")
	}
	if ts.declareAsks != 1 {
		t.Errorf("the budget restarted at %d, not 1", ts.declareAsks)
	}
}

// …but a tool CALL is not work. An agent that cannot produce the declaration answers every
// reminder with one, so crediting calls would make the budget infinite for exactly the case it
// exists to bound. Only a real file mutation moves the epoch, and an idempotent rewrite does not.
func TestBusyworkSinceTheLastAskDoesNotRestartTheBudget(t *testing.T) {
	fc := &fakeCouncil{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()
	g := newRunGuard()
	tc := turnCtx{s: a.sessionInfo(ctx, sid), agent: AgentSpec{Name: "coder"}, guard: g}
	ts := &turnState{}

	g.mutated("/app/x.c", "same") // one real write, before any ask
	for i := 0; i < declareAskCap; i++ {
		g.check("bash", json.RawMessage(`{"command":"ls"}`)) // busy between reminders
		g.mutated("/app/x.c", "same")                        // …and rewriting identical content
		if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); !done {
			t.Fatalf("ask %d was not made", i+1)
		}
	}
	if _, done := a.requireFinishDeclaration(ctx, tc, true, ts); done {
		t.Error("tool calls and idempotent rewrites bought an unbounded turn")
	}
	if !strings.Contains(ts.unverifiedReason, "never declared") {
		t.Errorf("ending undeclared must still be recorded, got %q", ts.unverifiedReason)
	}
}

// The rebuttal round is the only thing that can make the recorded verdicts differ from what the
// members first thought, and it leaves no other trace: the verdicts are emitted post-debate with
// the round hardcoded to 1. Without the outcome on the fact, a 3-0 that started 1-2 is
// indistinguishable from three members who agreed immediately — so whether debating changes
// anything, which is the only argument for its extra calls, cannot be asked of a run.
func TestAcceptedDeclarationRecordsTheRebuttal(t *testing.T) {
	decidedFact := func(t *testing.T, d council.Deliberation) event.CouncilDecidedData {
		t.Helper()
		fc := &fakeCouncil{delibs: []council.Deliberation{d}}
		a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
		ctx := context.Background()
		if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "", true); err != nil {
			t.Fatalf("declaring completion: %v", err)
		}
		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range evs {
			if e.Type != event.TypeCouncilDecided {
				continue
			}
			var got event.CouncilDecidedData
			if json.Unmarshal(e.Data, &got) == nil {
				return got
			}
		}
		t.Fatal("no council.decided fact was recorded")
		return event.CouncilDecidedData{}
	}

	base := council.Deliberation{
		Round: 1, Decision: council.Done,
		Breakdown: council.Breakdown{Done: 3, Voters: 3, Rule: council.RuleMajority},
		Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Done},
			{Member: "Balthasar", Lens: "verification", Decision: council.Done},
			{Member: "Casper", Lens: "completeness", Decision: council.Done},
		},
	}

	// A rebuttal that turned the council around must survive onto the fact, arrows and all.
	debated := base
	debated.Debate = &council.DebateOutcome{Before: council.Continue, After: council.Done, Changed: 2}
	got := decidedFact(t, debated)
	if got.Debate == nil {
		t.Fatal("the rebuttal outcome was computed and dropped on the way to the fact")
	}
	if got.Debate.Before != council.Continue || got.Debate.After != council.Done || got.Debate.Changed != 2 {
		t.Errorf("rebuttal recorded wrong: %+v", *got.Debate)
	}

	// No rebuttal ran (the common case: the independent vote was already unanimous). The fact
	// must not imply one did — an omitted field and a zero-valued one read differently.
	if got := decidedFact(t, base); got.Debate != nil {
		t.Errorf("no debate ran, so the fact must carry none, got %+v", *got.Debate)
	}
}
