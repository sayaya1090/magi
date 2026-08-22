package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// The closing call is the only voice in a round with no verdict slot of its own, so the only way it
// reaches the agent is this renderer. It once did not, and the failure was silent in the worst way:
// the turn was turned back three times with a real defect named, and the agent read three member
// blocks all saying everything was satisfied.
func TestTheClosingCallReachesTheAgent(t *testing.T) {
	d := council.Deliberation{
		Decision: council.Continue,
		Close:    "the written value is far outside the range its subject admits",
		Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Done, Rationale: "format matches"},
			{Member: "Balthasar", Lens: "verification", Decision: council.Done, Rationale: "a real run produced it"},
			{Member: "Casper", Lens: "completeness", Decision: council.Done, Rationale: "every part is present"},
		},
	}
	out := renderCouncilAdvice(d, "What the members said:")
	if !strings.Contains(out, d.Close) {
		t.Fatalf("the closing call is missing from what the agent reads:\n%s", out)
	}
	// Above the members, because the members it overruled are each about to say at length that
	// nothing is wrong, and whichever the agent reads first is the one it acts on.
	if strings.Index(out, d.Close) > strings.Index(out, "Melchior") {
		t.Fatalf("the closing call must come before the members it overruled:\n%s", out)
	}
	// And marked as the thing in the way, or it reads as a fourth opinion among three acceptances.
	if !strings.Contains(out, "stands in the way") {
		t.Fatalf("a continue must say the closing call is what blocks it:\n%s", out)
	}
}

// When it agreed, it still travels — but it must not be dressed as an objection.
func TestAnAgreeingClosingCallIsNotDressedAsAnObjection(t *testing.T) {
	d := council.Deliberation{
		Decision: council.Done,
		Close:    "the three walks hold up against each other",
		Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Decision: council.Done, Rationale: "ok"}},
	}
	out := renderCouncilAdvice(d, "What the members said:")
	if !strings.Contains(out, d.Close) {
		t.Fatalf("an agreeing closing call should still be readable:\n%s", out)
	}
	if strings.Contains(out, "stands in the way") {
		t.Fatalf("nothing is in the way of a done:\n%s", out)
	}
}

// An empty one adds no heading: a round whose closing call never ran must not look like one whose
// closing call had nothing to say.
func TestNoClosingCallAddsNoBlock(t *testing.T) {
	d := council.Deliberation{
		Decision: council.Continue,
		Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Decision: council.Continue, Feedback: "fix it"}},
	}
	if out := renderCouncilAdvice(d, "What the members said:"); strings.Contains(out, "closing call") {
		t.Fatalf("no closing call ran, so no closing-call block:\n%s", out)
	}
}
