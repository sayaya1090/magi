package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// The three council paths mean three different things, and the block of member readings is shared
// between them. Its lead used to be fixed at "this is their reading, not a decision — weigh it and
// judge for yourself", which is right when the agent merely asked and contradicts the line directly
// above it on the other two: the accept path has just ended the turn, and the reject path has just
// told the agent to address what follows.
func TestCouncilAdviceLeadMatchesThePath(t *testing.T) {
	d := council.Deliberation{
		Decision: council.Done,
		Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Feedback: "it holds"}},
	}
	const notADecision = "not a decision"

	advisory := renderCouncilAdvice(d,
		"The council read your work. This is their reading, not a decision — weigh it and judge for yourself.")
	if !strings.Contains(advisory, notADecision) {
		t.Errorf("the advisory path should still say it is not a decision:\n%s", advisory)
	}
	for _, lead := range []string{"What the members said:"} {
		out := renderCouncilAdvice(d, lead)
		if strings.Contains(out, notADecision) {
			t.Errorf("a deciding path must not also call itself no decision:\n%s", out)
		}
		if !strings.HasPrefix(out, lead) {
			t.Errorf("the lead should open the block, got:\n%s", out)
		}
		if !strings.Contains(out, "it holds") {
			t.Errorf("the members' words are missing:\n%s", out)
		}
	}
}
