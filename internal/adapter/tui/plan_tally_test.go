package tui

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// The line under a plan-audit round, and the only place the severities a member gave are turned
// into words a reader sees. It sat at 9.5% coverage: the empty case and one branch. Every tier
// below is reachable from a member's own reply, and a tier that counted into the wrong bucket
// would read as a milder round than the one that happened.
func TestThePlanTallyReadsBackWhatTheMembersSaid(t *testing.T) {
	v := func(dec, sev string) event.CouncilVerdictData {
		return event.CouncilVerdictData{Decision: dec, Severity: sev}
	}
	for _, c := range []struct {
		name string
		in   []event.CouncilVerdictData
		want string
	}{
		{"nothing to count", nil, ""},
		{"all approve", []event.CouncilVerdictData{v("done", ""), v("done", "")}, "2 approve"},
		{"critical is revise", []event.CouncilVerdictData{v("done", ""), v("continue", "critical")}, "1 approve / 1 revise"},
		{"warn is advise", []event.CouncilVerdictData{v("done", ""), v("continue", "warn")}, "1 approve / 1 advise"},
		{"info is note", []event.CouncilVerdictData{v("done", ""), v("continue", "info")}, "1 approve / 1 note"},
		{"abstain is its own", []event.CouncilVerdictData{v("done", ""), v("abstain", "")}, "1 approve / 1 abstain"},
		// A continue with no severity, and one with a severity nobody defined, both land in
		// revise — the label a member's unsoftened objection deserves.
		{"unset severity", []event.CouncilVerdictData{v("continue", "")}, "0 approve / 1 revise"},
		{"unknown severity", []event.CouncilVerdictData{v("continue", "catastrophic")}, "0 approve / 1 revise"},
		{"every tier at once", []event.CouncilVerdictData{
			v("done", ""), v("continue", "critical"), v("continue", "warn"),
			v("continue", "info"), v("abstain", "")},
			"1 approve / 1 revise / 1 advise / 1 note / 1 abstain"},
	} {
		if got := planTierTally(c.in); got != c.want {
			t.Errorf("%s: planTierTally = %q, want %q", c.name, got, c.want)
		}
	}
}

// The approve count is always there, so a round with nothing but objections still says how many
// members were for it — zero — rather than leaving the reader to infer it from an absence.
func TestTheTallyAlwaysStatesTheApproveCount(t *testing.T) {
	got := planTierTally([]event.CouncilVerdictData{{Decision: "continue", Severity: "warn"}})
	if got == "" || got[:9] != "0 approve" {
		t.Errorf("a round with no approvals reads as %q", got)
	}
}
