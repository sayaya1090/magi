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
