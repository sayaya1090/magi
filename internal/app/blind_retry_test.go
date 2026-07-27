package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// A retry that re-sends a byte-identical prompt asks the same model the same question and mostly
// receives the same answer — it spends a call to re-observe a failure rather than to correct it.
// Both retries below already had the diagnosis in hand: one logged it and threw it away, the other
// never looked. What they send now differs from the first attempt by exactly that diagnosis.

func TestRePlanRetryIsToldTheFirstReplyWasUnusable(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue,
				Severity: council.SeverityCritical, Feedback: "add a step that runs the suite"},
		}},
		{Round: 2, Decision: council.Done},
	}}
	// The re-plan comes back carrying no step at all, twice — runPlanner's own JSON-only retry
	// absorbs the first, so reaching the audit's retry takes two. The audit's retry then answers
	// properly.
	calls := 0
	llm := &recLLM{reply: func(string) string {
		calls++
		if calls <= 2 {
			return `{"steps":[],"reason":"I have nothing to change"}`
		}
		return `{"steps":[{"title":"run the suite","strategy":"solo"}]}`
	}}
	a, wd := newApp(t, llm, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	llm.mu.Lock()
	defer llm.mu.Unlock()
	retry := ""
	for _, p := range llm.prompts {
		if strings.Contains(p, "previous reply was unusable") {
			retry = p
		}
	}
	if retry == "" {
		t.Fatalf("the retry re-sent the same prompt with no word of what went wrong; %d planner call(s)", len(llm.prompts))
	}
	if strings.Contains(llm.prompts[0], "previous reply was unusable") {
		t.Errorf("the FIRST re-plan was told it was a retry:\n%s", llm.prompts[0])
	}
	// The retry is still a revision: dropping the critique to make room for the retry notice would
	// trade one blind call for another.
	if !strings.Contains(retry, "add a step that runs the suite") {
		t.Errorf("the retry lost the council feedback it exists to address:\n%s", retry)
	}
}

// The distill retry names the defect by SHAPE. "Reply with only JSON" is the wrong correction for a
// bare-but-malformed object, and equally wrong for well-formed JSON under the wrong keys — a model
// told the wrong thing tends to repeat the right thing it already did.
func TestDistillRetryReminderNamesTheActualDefect(t *testing.T) {
	syntax := distillRetryReminder(`{"lines":[{"surface":"a"}, "final":"x"}`)
	if !strings.Contains(syntax, "the JSON") || strings.Contains(syntax, "no prose, explanation, or markdown fence") {
		t.Errorf("a malformed object was told to remove prose it never wrote:\n%s", syntax)
	}
	shape := distillRetryReminder(`{"summary":"I looked at the request","notes":[]}`)
	if !strings.Contains(shape, "`lines`") {
		t.Errorf("well-formed JSON with the wrong keys was not told which keys:\n%s", shape)
	}
	prose := distillRetryReminder("Sure! Here is my analysis of the request.")
	if !strings.Contains(prose, "ONLY the JSON object") {
		t.Errorf("a prose reply was not asked for bare JSON:\n%s", prose)
	}
	// Every branch has to say why it matters: the model cannot otherwise know that a dropped note
	// is not a neutral outcome.
	for _, r := range []string{syntax, shape, prose} {
		if !strings.Contains(r, "not neutral") {
			t.Errorf("a retry reminder omitted the stakes:\n%s", r)
		}
	}
}
