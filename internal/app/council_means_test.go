package app

import (
	"strings"
	"testing"
)

// meansHint selects a recipe by keyword and stays silent when nothing matches.
func TestMeansHintServerCategory(t *testing.T) {
	// The pypi-server objection: keep a live server up so pip can install from it.
	h := meansHint("Missing evidence that the PyPI server is running on port 8080")
	if h == "" {
		t.Fatal("server/running feedback should yield a means recipe")
	}
	if !strings.Contains(h, "setsid") || !strings.Contains(h, "curl") {
		t.Fatalf("server recipe should teach detached launch + liveness check, got:\n%s", h)
	}
}

func TestMeansHintEvidenceCategory(t *testing.T) {
	h := meansHint("The report claims success but provides no proof; demonstrate it actually works")
	if h == "" || !strings.Contains(h, "end-to-end") {
		t.Fatalf("evidence feedback should yield the run-the-real-command recipe, got:\n%q", h)
	}
}

// A generic objection with none of the operational keywords gets no hint — the objection is
// injected unchanged, so the hint never becomes noise on unrelated feedback.
func TestMeansHintNoMatch(t *testing.T) {
	if h := meansHint("The variable name should be more descriptive"); h != "" {
		t.Fatalf("unrelated feedback must yield no hint, got:\n%s", h)
	}
}

// Means escalation is ON by default (round-cost reduction: the recipe rides the
// FIRST rejection); MAGI_COUNCIL_MEANS=off is the A/B knob to reproduce the
// historical plain-objection feedback.
func TestCouncilMeansEnabledGate(t *testing.T) {
	t.Setenv("MAGI_COUNCIL_MEANS", "")
	if !councilMeansEnabled() {
		t.Fatal("means escalation must be ON by default")
	}
	t.Setenv("MAGI_COUNCIL_MEANS", "1")
	if !councilMeansEnabled() {
		t.Fatal("MAGI_COUNCIL_MEANS=1 keeps it enabled")
	}
	t.Setenv("MAGI_COUNCIL_MEANS", "off")
	if councilMeansEnabled() {
		t.Fatal("MAGI_COUNCIL_MEANS=off must disable it")
	}
}
