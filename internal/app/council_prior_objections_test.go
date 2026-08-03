package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

func verdictEv(member, lens, decision, feedback string) event.Event {
	b, _ := json.Marshal(event.CouncilVerdictData{
		Round: 1, Member: member, Lens: lens, Decision: decision, Feedback: feedback,
	})
	return event.Event{Type: event.TypeCouncilVerdict, Data: b}
}

// The council's own words used to reach it only as tool results, which are kept to the most recent
// few. Measured on a run that failed: five deliberations, each able to see at most the one before
// it. Round 2 named the exact defect the graded test later failed on; rounds 3, 4 and 5 never saw
// that sentence again, and round 5 accepted.
func TestTheCouncilIsHandedItsOwnEarlierObjections(t *testing.T) {
	evs := []event.Event{
		event.Event{Type: event.TypePromptSubmitted},
		verdictEv("Melchior", "correctness", "continue",
			"a task cancelled while waiting on the semaphore never reaches its finally"),
		verdictEv("Casper", "completeness", "done", "looks complete to me"),
		verdictEv("Balthasar", "verification", "continue", "the interrupt path was never actually run"),
	}
	got := priorCouncilObjections(evs, 6, 4000)
	if !strings.Contains(got, "waiting on the semaphore") {
		t.Errorf("the objection must survive to the next round:\n%s", got)
	}
	if !strings.Contains(got, "Melchior (correctness)") {
		t.Errorf("whose lens said it is part of what was said:\n%s", got)
	}
	// An APPROVING verdict is not an objection: handing it back would read as a standing complaint.
	if strings.Contains(got, "looks complete") {
		t.Errorf("a done vote is not an unmet concern:\n%s", got)
	}
	// Most recent first, so a cap drops the oldest rather than the freshest.
	if i, j := strings.Index(got, "never actually run"), strings.Index(got, "semaphore"); i > j {
		t.Errorf("most recent objection must lead:\n%s", got)
	}
}

// Objections belong to the work they judged. A new user prompt is new work, and carrying the last
// task's complaints into it would have the council answering a question nobody asked.
func TestObjectionsDoNotCrossTurns(t *testing.T) {
	// A turn boundary is a USER prompt; magi's own injected prompts carry a system actor.
	userPrompt := event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}}
	evs := []event.Event{
		userPrompt,
		verdictEv("Melchior", "correctness", "continue", "the previous task's parser is wrong"),
		userPrompt,
		verdictEv("Casper", "completeness", "continue", "this task has no test at all"),
	}
	got := priorCouncilObjections(evs, 6, 4000)
	if strings.Contains(got, "previous task") {
		t.Errorf("an earlier turn's objection must not follow the next one:\n%s", got)
	}
	if !strings.Contains(got, "no test at all") {
		t.Errorf("this turn's objection must be there:\n%s", got)
	}
	// Nothing to hand back is silence, not an empty heading.
	if got := priorCouncilObjections([]event.Event{userPrompt}, 6, 4000); got != "" {
		t.Errorf("a first round has no history and must say nothing, got %q", got)
	}
}

// The same member repeating itself across rounds is one concern, not three: the block says what is
// outstanding, and a triplicate reads as three separate problems.
func TestRepeatedObjectionsCollapse(t *testing.T) {
	same := "the interrupt path was never actually run"
	evs := []event.Event{
		event.Event{Type: event.TypePromptSubmitted},
		verdictEv("Balthasar", "verification", "continue", same),
		verdictEv("Balthasar", "verification", "continue", same),
		verdictEv("Balthasar", "verification", "continue", same),
	}
	if n := strings.Count(priorCouncilObjections(evs, 6, 4000), same); n != 1 {
		t.Errorf("one concern, stated once — got it %d times", n)
	}
}
