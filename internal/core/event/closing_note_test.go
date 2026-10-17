package event

import (
	"github.com/sayaya1090/magi/internal/core/council"
	"strings"
	"testing"
)

func TestClosingReviewIsSeparateFromMemberVote(t *testing.T) {
	d := CouncilDecidedData{Decision: "continue", ClosingDecision: "continue", ClosingReason: "verify 72dp", Tally: council.Breakdown{Done: 3, Voters: 3}}
	if got := d.OutcomeNote(); !strings.Contains(got, "final review: held") || !strings.Contains(got, "72dp") {
		t.Fatal(got)
	}
	d.ClosingDecision = ""
	d.ClosingReason = ""
	d.Feedback = "verify 72dp"
	if got := d.OutcomeNote(); !strings.Contains(got, "legacy record") || !strings.Contains(got, "72dp") {
		t.Fatal(got)
	}
	d.Tally.Continue = 1
	d.Note = "members disagreed"
	if d.OutcomeNote() != d.Note {
		t.Fatal("ordinary rejection mislabelled")
	}
}
