package app

import (
	"strings"
	"testing"
)

// criteriaPerItemEnabled defaults ON; explicit off values restore the single-block rendering.
func TestCriteriaPerItemFlag(t *testing.T) {
	t.Setenv("MAGI_CRITERIA_PERITEM", "")
	if !criteriaPerItemEnabled() {
		t.Error("default must be ON")
	}
	for _, off := range []string{"0", "off", "false", "no"} {
		t.Setenv("MAGI_CRITERIA_PERITEM", off)
		if criteriaPerItemEnabled() {
			t.Errorf("%q must disable", off)
		}
	}
}

// renderCriteriaChecklist turns a "- item" block into a NUMBERED per-item checklist with the
// judge-each-item instruction, so the termination council cannot wave a partly-met contract to done.
func TestRenderCriteriaChecklist(t *testing.T) {
	out := renderCriteriaChecklist("- the server answers SetVal\n-  returns the stored value\n- ")
	for _, want := range []string{
		"judge EACH item individually", "EVERY", "UNSATISFIED",
		"1. the server answers SetVal", "2. returns the stored value",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("checklist missing %q in:\n%s", want, out)
		}
	}
	// The blank trailing bullet must not become "3. ".
	if strings.Contains(out, "3.") {
		t.Errorf("empty criterion rendered as an item:\n%s", out)
	}
	// Empty input falls back to the plain block rather than emitting a bare instruction.
	if fb := renderCriteriaChecklist(""); !strings.Contains(fb, "Acceptance criteria") {
		t.Errorf("empty input should fall back to the plain block, got %q", fb)
	}
}
