package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// The `keep` clause was written for THIS phase — one member's critical flaw sends the WHOLE plan
// back to be re-planned, and the re-planner is otherwise free to drop the parts the other members
// had already blessed. The clause and its schema field only appear in the member prompt when the
// request asks for them, so a plan audit that forgets the flag silently disables the whole
// mechanism: no verdict can carry a `keep`, AggregateKeep is always empty, and the preserve branch
// is unreachable. That is not visible in any log — `keep` is not on the verdict EVENT — so the
// wiring is pinned here instead.
func TestPlanAuditAsksMembersWhatToKeep(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	if len(fc.reqs) == 0 {
		t.Fatal("the plan audit did not deliberate")
	}
	if !fc.reqs[0].Keep {
		t.Error("the plan audit must ask members for `keep`; without it the preserve-across-revisions " +
			"branch can never fire")
	}
	if fc.reqs[0].Phase != "plan" {
		t.Fatalf("phase = %q, want the plan audit", fc.reqs[0].Phase)
	}
}

// A/B: the flag has to reach the request too, or the "off" arm is not actually a baseline.
func TestPlanAuditKeepFollowsTheFlag(t *testing.T) {
	t.Setenv("MAGI_COUNCIL_KEEP", "0")
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	if len(fc.reqs) == 0 {
		t.Fatal("the plan audit did not deliberate")
	}
	if fc.reqs[0].Keep {
		t.Error("MAGI_COUNCIL_KEEP=0 must reach the plan audit request (byte-identical baseline prompt)")
	}
}

// End of the chain: a member's `keep` has to arrive in the RE-PLANNER's prompt, alongside the
// blocking feedback and marked as advice — otherwise a revision can satisfy the new demand by
// discarding a step the previous round had just gained. This pins the DOWNSTREAM half only: the
// fake supplies a `keep` whether or not the request asked for one, so the request flag itself is
// pinned by TestPlanAuditAsksMembersWhatToKeep.
func TestPlanAuditCarriesKeepIntoTheRePlannerPrompt(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue,
				Severity: council.SeverityCritical, Feedback: "add a step that runs the suite"},
			{Member: "Balthasar", Lens: "verification", Decision: council.Done,
				Keep: "the step that verifies the build completes without crashing"},
		}},
		{Round: 2, Decision: council.Done},
	}}
	llm := &recLLM{reply: func(string) string {
		return `{"steps":[{"title":"X","strategy":"solo"}]}`
	}}
	a, wd := newApp(t, llm, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	llm.mu.Lock()
	defer llm.mu.Unlock()
	var revise string
	for _, p := range llm.prompts {
		if strings.Contains(p, "add a step that runs the suite") {
			revise = p
		}
	}
	if revise == "" {
		t.Fatalf("no re-planner prompt carried the blocking feedback; prompts=%d", len(llm.prompts))
	}
	if !strings.Contains(revise, "verifies the build completes without crashing") {
		t.Errorf("the re-planner was told what to fix but not what to preserve:\n%s", revise)
	}
	// Advice, never a constraint — a fix that genuinely requires changing a kept step must stay legal.
	if !strings.Contains(revise, "advice, not a rule") {
		t.Errorf("the keep block must state it is advisory:\n%s", revise)
	}
}
