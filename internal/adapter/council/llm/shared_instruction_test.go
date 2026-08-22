package llm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// Every shape that asks for a verdict must ask for the SAME judgement, byte for byte.
//
// This guards a defect that was shipped once and caught on a bench: the walk, the route and the
// reply shape were written into one caller in full and RETOLD in shorter words in another. Two arms
// were then compared as though only their evidence differed — when one had been handed a summary of
// the other's rules, so nothing the comparison found could be attributed to the thing it was
// comparing. Shapes may differ in what a member is shown and in how many calls it takes to ask.
// They may not differ in what is asked.
func TestEveryShapeAsksForTheIdenticalJudgement(t *testing.T) {
	m := council.Member{Name: "Melchior", Lens: "correctness"}
	for _, keep := range []bool{false, true} {
		shared := fmt.Sprintf(councilCore, keepClauseFor(keep)) +
			fmt.Sprintf(councilGrounds, citeNoEvidence, verdictSchemaFor(keep))
		if !strings.Contains(memberSystem(m, "ship it", keep), shared) {
			t.Fatalf("keep=%v: the single-member prompt does not carry the shared instruction verbatim", keep)
		}
	}
	// The panel asks three at once and closes the round itself, but the judging text is the same
	// one — only the roster, the independence clause and the reply shape are its own.
	panelCore := fmt.Sprintf(councilCore, keepClauseFor(false))
	if !strings.Contains(panelPromptFor(council.DefaultMembers(), false), panelCore) {
		t.Fatal("the panel prompt carries a RETELLING of the judging instruction, not the instruction")
	}
}

// What may SETTLE a walk item is the rule the council was measured failing: three members once
// certified a whole task on one short success line printed by a script the agent had written for
// the occasion, and both of that task's tests then failed. It belongs to the shared core, so every
// shape carries it.
func TestTheSettlingRuleIsShared(t *testing.T) {
	m := council.Member{Name: "x", Lens: "verification"}
	single := memberSystem(m, "ship it", false)
	panel := panelPromptFor(council.DefaultMembers(), false)
	for _, want := range []string{"WHAT MAY SETTLE AN ITEM", "something a TOOL RETURNED",
		"never settles anything", "success banner"} {
		if !strings.Contains(single, want) {
			t.Errorf("the single-member prompt lost the settling rule: %q", want)
		}
		if !strings.Contains(panel, want) {
			t.Errorf("the panel prompt lost the settling rule: %q", want)
		}
	}
}
