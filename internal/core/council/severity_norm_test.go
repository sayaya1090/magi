package council

import "testing"

// TestSeverityOfNormalization pins the trim + case-folding in severityOf that the existing
// severity tests don't exercise (they pass exact-case constants). A model may emit "Critical"
// or " warn " — these must still map to the right tier, and a blank string must read as warn
// (absent), not fall through to the unknown→critical fail-safe.
func TestSeverityOfNormalization(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Critical ", SeverityCritical},
		{"CRITICAL", SeverityCritical},
		{"WARN", SeverityWarn},
		{" info ", SeverityInfo},
		{"   ", SeverityWarn}, // whitespace-only is "absent" → warn, not unknown→critical
	}
	for _, c := range cases {
		if got := severityOf(Verdict{Severity: c.in}); got != c.want {
			t.Errorf("severityOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A PRESENT but non-canonical severity token — a model signaling urgency in its own words — must fail
// SAFE to critical (blocking), NOT be silently downgraded. This is the opposite pole from an ABSENT
// severity (→ warn): the member clearly meant to raise a concern, so a word we don't recognize must
// block rather than be ignored. Locks the default branch of severityOf end-to-end through
// HasCriticalRevision, so a future edit that flips the fallback to warn is caught.
func TestSeverityOfUnknownFailsSafeToCritical(t *testing.T) {
	for _, tok := range []string{"blocker", "high", "urgent", "severe", "major", "P0"} {
		if got := severityOf(Verdict{Severity: tok}); got != SeverityCritical {
			t.Errorf("unrecognized severity %q must fail safe to critical, got %q", tok, got)
		}
	}
	// The fail-safe must actually block a plan when carried on a continue vote.
	if !HasCriticalRevision([]Verdict{{Member: "M", Decision: Continue, Severity: "blocker", Feedback: "x"}}) {
		t.Error("a continue vote with an unrecognized-but-urgent severity must block the plan")
	}
}
