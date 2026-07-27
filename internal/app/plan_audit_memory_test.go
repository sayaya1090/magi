package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// A plan-audit revision is a review loop, and for most of this gate's life only the JUDGING half
// of it had a memory: the council receives `Revision` (the prior plan, the planner's stated reason,
// the judge's ruling), while the re-planner — a side call with no session history, since a plan is
// carried inside a council event and reconstruct turns only prompts and appended parts into
// messages — received the critique alone under a header naming "your previous plan". A producer
// that cannot see what it produced treats the review as a fresh request: whatever the critique did
// not mention is not preserved but re-derived, and steps an earlier round gained get dropped.
func TestRePlannerSeesThePlanItIsRevising(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue,
				Severity: council.SeverityCritical, Feedback: "add a step that runs the suite"},
		}},
		{Round: 2, Decision: council.Done},
	}}
	llm := &recLLM{reply: func(string) string { return `{"steps":[{"title":"X","strategy":"solo"}]}` }}
	a, wd := newApp(t, llm, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, _ := planAuditFixture(t, a, wd)
	// Titles that appear NOWHERE else — not in the request, not in the critique — so containment
	// can only be satisfied by the plan block itself.
	steps := []planStep{
		{Title: "vendor the zlib headers", Strategy: "solo"},
		{Title: "regenerate the parser tables", Strategy: "solo"},
	}
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	revise := rePlannerPrompt(t, llm, "add a step that runs the suite")
	if !strings.Contains(revise, "it is NOT in the conversation above") {
		t.Errorf("the re-plan instruction carries no prior-plan block:\n%s", revise)
	}
	for _, title := range []string{"vendor the zlib headers", "regenerate the parser tables"} {
		if !strings.Contains(revise, title) {
			t.Errorf("step %q of the plan being revised never reached the re-planner:\n%s", title, revise)
		}
	}
}

// The judge's ruling is the other half. When a rewrite is ruled NOT to engage the concern, the
// default (converge-stop off) sends it back for another round — and the re-planner met the same
// critique with no record that its last answer had already been rejected, so it re-submitted it.
// Observed live: three rounds, the second and third revisions byte-identical.
func TestRePlannerIsToldItsLastRewriteWasRuledUnresponsive(t *testing.T) {
	fc := &fakeCouncil{
		delibs: []council.Deliberation{{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue,
				Severity: council.SeverityCritical, Feedback: "add a step that runs the suite"},
		}}},
		judge: func(port.RevisionJudgeRequest) port.RevisionVerdict {
			return port.RevisionVerdict{Addressed: false, Reason: "no step actually runs anything"}
		},
	}
	llm := &recLLM{reply: func(string) string { return `{"steps":[{"title":"X","strategy":"solo"}]}` }}
	a, wd := newApp(t, llm, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	// Round 1's re-plan cannot know a verdict that does not exist yet; round 2's must.
	llm.mu.Lock()
	var withJudge int
	for _, p := range llm.prompts {
		if strings.Contains(p, "no step actually runs anything") {
			withJudge++
		}
	}
	n := len(llm.prompts)
	llm.mu.Unlock()
	if withJudge == 0 {
		t.Fatalf("the unresponsive-revision ruling never reached the re-planner; %d planner call(s)", n)
	}
}

// The converse: a rewrite the judge ACCEPTED must not be reported back as a failure. "You engaged
// the concern" gives the next rewrite nothing to act on, and phrasing it as a rejection would push
// the planner off a revision that was working.
func TestAnAddressedRevisionIsNotReportedAsUnresponsive(t *testing.T) {
	fc := &fakeCouncil{
		delibs: []council.Deliberation{{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue,
				Severity: council.SeverityCritical, Feedback: "add a step that runs the suite"},
		}}},
		judge: func(port.RevisionJudgeRequest) port.RevisionVerdict {
			return port.RevisionVerdict{Addressed: true, Reason: "the new step runs the suite"}
		},
	}
	llm := &recLLM{reply: func(string) string { return `{"steps":[{"title":"X","strategy":"solo"}]}` }}
	a, wd := newApp(t, llm, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	llm.mu.Lock()
	defer llm.mu.Unlock()
	for i, p := range llm.prompts {
		if strings.Contains(p, "judged NOT to engage") {
			t.Fatalf("planner call %d was told its accepted revision was unresponsive:\n%s", i, p)
		}
	}
}

// A FIRST plan has nothing to remember: the zero replanContext must leave the prompt as it was, so
// the initial plan is not preceded by an empty "your previous plan" heading.
func TestFirstPlanCarriesNoRevisionMemory(t *testing.T) {
	cfg := Config{Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}}
	llm := &recLLM{reply: func(string) string { return `{"steps":[{"title":"x","strategy":"solo"}]}` }}
	a, wd := newApp(t, llm, cfg)
	a.runPlanner(context.Background(), cfg.Agents[plannerAgent], parentSession(wd), "build a thing", "", replanContext{}, 0, 30, "")

	llm.mu.Lock()
	defer llm.mu.Unlock()
	for _, p := range llm.prompts {
		if strings.Contains(p, "it is NOT in the conversation above") || strings.Contains(p, "judged NOT to engage") {
			t.Errorf("a first plan was given revision memory it does not have:\n%s", p)
		}
	}
}

// rePlannerPrompt returns the recorded prompt that carried the given critique.
func rePlannerPrompt(t *testing.T, llm *recLLM, critique string) string {
	t.Helper()
	llm.mu.Lock()
	defer llm.mu.Unlock()
	for _, p := range llm.prompts {
		if strings.Contains(p, critique) {
			return p
		}
	}
	t.Fatalf("no re-planner prompt carried the blocking feedback; prompts=%d", len(llm.prompts))
	return ""
}
