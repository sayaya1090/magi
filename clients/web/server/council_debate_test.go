package main

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/app"
)

// A rebuttal round runs only when the independent vote split, and the tally recorded beside it is
// the one taken AFTER it. So the console's "3 done of 3" can be the product of an argument rather
// than of agreement — and the console was the one surface with no way to say so: the field reached
// the TUI verdict line, the headless transcript and the loop map, and stopped before the wire.
func TestConsoleSaysWhetherTheRoundWasArgued(t *testing.T) {
	quiet := councilText(app.CouncilMark{Round: 1, Decision: "done", Tally: "3 done, 0 continue of 3"})
	if strings.Contains(quiet, "debated") {
		t.Errorf("nobody argued, so the row must not say they did: %q", quiet)
	}
	argued := councilText(app.CouncilMark{Round: 1, Decision: "done", Tally: "3 done, 0 continue of 3",
		Debate: "debated: continue→done, 2 members moved"})
	if !strings.Contains(argued, "continue→done") || !strings.Contains(argued, "2 members moved") {
		t.Errorf("the same tally, reached by argument, and the row must say which: %q", argued)
	}
	// Still readable with the outcome's note under it — the note is the body, this belongs on the
	// summary line the page folds the rest behind.
	withNote := councilText(app.CouncilMark{Round: 1, Decision: "done", Tally: "3 done, 0 continue of 3",
		Debate: "debated, no one moved", Why: "the agent declared the task finished and the council accepts"})
	head, _, ok := strings.Cut(withNote, "\n\n")
	if !ok || !strings.Contains(head, "no one moved") {
		t.Errorf("the debate belongs on the summary line, not in the body: %q", withNote)
	}
}
