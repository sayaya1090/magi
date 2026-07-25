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

// A word that merely CONTAINS "port" — export, support, important, report — must NOT trigger the
// server recipe. Category 1 matches " port" (leading space) precisely to reject those substrings; a
// regression to a bare "port" match would fire the recipe on unrelated prose.
func TestMeansHintPortSubstringNoFalsePositive(t *testing.T) {
	for _, fb := range []string{
		"please add export documentation",
		"this refactor is important for clarity",
		"the support ticket was closed",
	} {
		if h := meansHint(fb); h != "" {
			t.Errorf("feedback %q merely contains 'port' — must not yield a recipe, got:\n%s", fb, h)
		}
	}
	// The genuine case still matches: a live listener on a port.
	if h := meansHint("the service must respond on port 8080"); !strings.Contains(h, "setsid") {
		t.Errorf("a real ' port' objection must still yield the server recipe, got:\n%s", h)
	}
}

// Feedback that hits BOTH categories carries both recipes, joined under one header.
func TestMeansHintBothCategories(t *testing.T) {
	h := meansHint("no evidence the server is running; prove it actually installs")
	if !strings.Contains(h, "setsid") || !strings.Contains(h, "end-to-end") {
		t.Fatalf("both the server and the evidence recipe should appear, got:\n%s", h)
	}
	if strings.Count(h, "Means (task-agnostic") != 1 {
		t.Errorf("the two recipes must share a single header, got:\n%s", h)
	}
}
