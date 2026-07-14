package app

import (
	"strings"
	"testing"
)

func TestImplicitAcceptEnabledDefaultOff(t *testing.T) {
	t.Setenv("MAGI_IMPLICIT_ACCEPT", "")
	if implicitAcceptEnabled() {
		t.Fatal("MAGI_IMPLICIT_ACCEPT unset must be OFF (opt-in)")
	}
	t.Setenv("MAGI_IMPLICIT_ACCEPT", "1")
	if !implicitAcceptEnabled() {
		t.Fatal("MAGI_IMPLICIT_ACCEPT=1 must be ON")
	}
	t.Setenv("MAGI_IMPLICIT_ACCEPT", "no")
	if implicitAcceptEnabled() {
		t.Fatal("MAGI_IMPLICIT_ACCEPT=no must be OFF")
	}
}

func TestImplicitAcceptRuleContent(t *testing.T) {
	// The rule must name the three levers it steers on, so a prompt refactor that drops one
	// is caught here rather than silently weakening the arm.
	for _, want := range []string{"NOT shown to you", "EXACT output", "STANDARD SEMANTICS", "IDIOMATIC"} {
		if !strings.Contains(implicitAcceptRule, want) {
			t.Errorf("implicitAcceptRule missing %q", want)
		}
	}
}
