package llm

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The council judges against the same rules the agent was handed.
//
// Without the operator's standing instructions a member sees only the literal wording of the
// request, so obeying a standing rule reads as departing from what was asked. Observed live
// (2026-09-02): the rule was "slide titles must be wrapped in brackets", the person asked for a
// slide called 내년 목표, the agent correctly created "[내년 목표]", the council rejected the turn,
// and the agent then edited the brackets back out to satisfy it.
//
// A standing instruction that is followed and then reverted is worse than one that is ignored: the
// person sees the rule work sometimes and cannot tell when.
func TestTheCouncilIsShownTheStandingInstructions(t *testing.T) {
	got := evidence(port.DeliberationRequest{
		Task:     "make a slide called 내년 목표",
		Report:   "created it",
		Actions:  `add_slide title="[내년 목표]"`,
		Standing: "슬라이드 제목은 반드시 대괄호로 감싼다",
	})

	if !strings.Contains(got, "슬라이드 제목은 반드시 대괄호로 감싼다") {
		t.Fatalf("the standing rule is not in the evidence a member reads:\n%s", got)
	}
	// It must also say what the rule MEANS for the vote. Text a member cannot act on is text a
	// member ignores — and the failure here is not that the rule was invisible but that following
	// it looked like a defect.
	for _, must := range []string{"CORRECT", "literal wording", "never require the agent to undo"} {
		if !strings.Contains(got, must) {
			t.Fatalf("the evidence does not tell a member how to weigh the rule (missing %q):\n%s", must, got)
		}
	}
	// Read with the task, not filed somewhere after the evidence: the request is what was asked
	// this time, the rule is what was already in force.
	if strings.Index(got, "standing instructions") > strings.Index(got, "Agent's report") {
		t.Fatal("the standing instructions come after the report — a member reads them too late to weigh the claim")
	}
}

// Most turns have none, and then nothing is added — an empty section teaches a member that rules
// exist and this turn had none, which is a different claim from silence.
func TestNoStandingInstructionsAddsNothing(t *testing.T) {
	got := evidence(port.DeliberationRequest{
		Task: "make a slide", Report: "done", Actions: "add_slide",
	})
	if strings.Contains(strings.ToLower(got), "standing instruction") {
		t.Fatalf("an empty section was written anyway:\n%s", got)
	}
}

// Whitespace-only is the same as none — a file with a blank line in it is not a rule.
func TestBlankStandingInstructionsAddNothing(t *testing.T) {
	got := evidence(port.DeliberationRequest{
		Task: "make a slide", Report: "done", Actions: "add_slide", Standing: "  \n\t\n ",
	})
	if strings.Contains(strings.ToLower(got), "standing instruction") {
		t.Fatalf("blank text was treated as a rule:\n%s", got)
	}
}
