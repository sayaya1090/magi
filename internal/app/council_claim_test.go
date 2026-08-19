package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func saidEvents(texts ...string) []event.Event {
	out := make([]event.Event, 0, len(texts))
	for _, t := range texts {
		b, _ := json.Marshal(event.PartAppendedData{
			Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: t}})
		out = append(out, event.Event{Type: event.TypePartAppended, Data: b})
	}
	return out
}

// The report reaches the council even when the declaration is a separate message.
//
// Only the LAST assistant text was taken, and the last thing an agent says is very often not its
// report — the orchestrator's own reminder asks it to "call the council tool with complete: true",
// which invites a short message of its own. Reported from live use and reproduced exactly: an
// agent wrote its report, then said "Requesting council review.", and the members were handed the
// second line as the claim. They answered that there was no report, which was true of what they
// were shown and false of what had happened.
func TestTheReportSurvivesASeparateDeclaration(t *testing.T) {
	report := "REPORT\n\nI examined the three services and found the retry loop in billing: " +
		strings.Repeat("the handler retries without a ceiling. ", 12)
	got := lastTurnAssistantText(saidEvents(report, "Requesting council review."))
	if !strings.Contains(got, "retry loop in billing") {
		t.Fatalf("the council was shown %q — the report the agent wrote is not in it", got)
	}
	if !strings.Contains(got, "Requesting council review.") {
		t.Error("the declaration itself was dropped; it is the newest thing said and the members " +
			"should see what was claimed as well as what was done")
	}
}

// A report that IS the last message is left exactly as it was written.
func TestAReportThatStandsAloneIsNotPaddedWithHistory(t *testing.T) {
	report := "REPORT\n\n" + strings.Repeat("a finding worth reading. ", 40)
	got := lastTurnAssistantText(saidEvents("Let me look at the billing service first.", report))
	if strings.Contains(got, "Let me look at the billing service first.") {
		t.Error("an earlier working note was folded into a report that needed no help; the claim " +
			"should be what the agent last said when that is a real answer")
	}
	if got != report {
		t.Errorf("the report was altered: %q", got)
	}
}

// It stays bounded. The same trials hold a median 7.5KB and a p90 of 20KB of assistant text; the
// claim must not be able to crowd out magi's own record, which is the part the agent did not write.
func TestTheAssembledClaimStaysWithinItsBudget(t *testing.T) {
	var texts []string
	for i := 0; i < 30; i++ {
		texts = append(texts, strings.Repeat("x", 2000))
	}
	texts = append(texts, "done")
	got := lastTurnAssistantText(saidEvents(texts...))
	if len(got) > claimCap+16 {
		t.Errorf("the claim came to %d bytes against a cap of %d", len(got), claimCap)
	}
	if !strings.HasSuffix(got, "done") {
		t.Error("the newest thing said is not at the end; the declaration is what the members are judging")
	}
}

// Nothing said, nothing claimed — and no crash on the empty turn.
func TestNoTextMeansNoClaim(t *testing.T) {
	if got := lastTurnAssistantText(nil); got != "" {
		t.Errorf("an empty turn produced a claim of %q", got)
	}
}
