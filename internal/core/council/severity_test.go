package council

import (
	"strings"
	"testing"
)

func cont(member, sev, fb string) Verdict {
	return Verdict{Member: member, Decision: Continue, Severity: sev, Feedback: fb}
}

// Only a critical "continue" blocks the plan gate; warn/info (and a missing severity,
// which defaults to warn) are advisory. A single critical vetoes.
func TestHasCriticalRevision(t *testing.T) {
	cases := []struct {
		name string
		vs   []Verdict
		want bool
	}{
		{"critical continue blocks", []Verdict{cont("M", SeverityCritical, "x")}, true},
		{"warn+info do not block", []Verdict{cont("M", SeverityWarn, "x"), cont("C", SeverityInfo, "y")}, false},
		{"missing severity defaults to warn", []Verdict{cont("M", "", "x")}, false},
		{"unknown severity defaults to warn", []Verdict{cont("M", "huge", "x")}, false},
		{"done is never a blocking revision", []Verdict{{Member: "M", Decision: Done, Severity: SeverityCritical}}, false},
		{"a single critical vetoes among warns", []Verdict{cont("A", SeverityWarn, "a"), cont("B", SeverityCritical, "b")}, true},
		{"empty → no block", nil, false},
	}
	for _, c := range cases {
		if got := HasCriticalRevision(c.vs); got != c.want {
			t.Errorf("%s: HasCriticalRevision = %v, want %v", c.name, got, c.want)
		}
	}
}

// CriticalFeedback and AdvisoryFeedback partition the continue feedback by severity.
func TestSeverityFeedbackPartition(t *testing.T) {
	vs := []Verdict{
		{Member: "Melchior", Lens: "correctness", Decision: Continue, Severity: SeverityCritical, Feedback: "missing build step"},
		{Member: "Casper", Lens: "completeness", Decision: Continue, Severity: SeverityWarn, Feedback: "could add a test"},
		{Member: "Balthasar", Decision: Continue, Severity: SeverityInfo, Feedback: "rename for clarity"},
		{Member: "X", Decision: Done},
	}
	crit := CriticalFeedback(vs)
	if !strings.Contains(crit, "missing build step") {
		t.Errorf("critical feedback should include the critical item: %q", crit)
	}
	if strings.Contains(crit, "could add a test") || strings.Contains(crit, "rename for clarity") {
		t.Errorf("critical feedback must exclude warn/info items: %q", crit)
	}
	adv := AdvisoryFeedback(vs)
	if !strings.Contains(adv, "could add a test") || !strings.Contains(adv, "rename for clarity") {
		t.Errorf("advisory feedback should include warn+info items: %q", adv)
	}
	if strings.Contains(adv, "missing build step") {
		t.Errorf("advisory feedback must exclude the critical item: %q", adv)
	}
	// Labels carry the member (and lens when present).
	if !strings.Contains(adv, "Casper (completeness)") {
		t.Errorf("advisory feedback should label the member/lens: %q", adv)
	}
}

// Empty when nothing matches the tier.
func TestSeverityFeedbackEmpty(t *testing.T) {
	done := []Verdict{{Member: "M", Decision: Done}}
	if CriticalFeedback(done) != "" || AdvisoryFeedback(done) != "" {
		t.Error("no continue verdicts → both feedback strings empty")
	}
	// A critical-only set yields no advisory; a warn-only set yields no critical.
	if AdvisoryFeedback([]Verdict{cont("M", SeverityCritical, "x")}) != "" {
		t.Error("critical-only → no advisory")
	}
	if CriticalFeedback([]Verdict{cont("M", SeverityWarn, "x")}) != "" {
		t.Error("warn-only → no critical")
	}
}
