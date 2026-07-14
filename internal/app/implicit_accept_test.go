package app

import (
	"strings"
	"testing"
)

func TestImplicitAcceptEnabledDefaultOn(t *testing.T) {
	t.Setenv("MAGI_IMPLICIT_ACCEPT", "")
	if !implicitAcceptEnabled() {
		t.Fatal("MAGI_IMPLICIT_ACCEPT unset must default ON")
	}
	t.Setenv("MAGI_IMPLICIT_ACCEPT", "1")
	if !implicitAcceptEnabled() {
		t.Fatal("MAGI_IMPLICIT_ACCEPT=1 must be ON")
	}
	t.Setenv("MAGI_IMPLICIT_ACCEPT", "off")
	if implicitAcceptEnabled() {
		t.Fatal("MAGI_IMPLICIT_ACCEPT=off must be OFF")
	}
	t.Setenv("MAGI_IMPLICIT_ACCEPT", "no")
	if implicitAcceptEnabled() {
		t.Fatal("MAGI_IMPLICIT_ACCEPT=no must be OFF")
	}
}

func TestImplicitAcceptRuleContent(t *testing.T) {
	// The rule must name the levers it steers on, so a prompt refactor that drops one
	// is caught here rather than silently weakening the arm. Framed generally (edge-case
	// rigor / careful-reviewer scrutiny), NOT as a hidden benchmark grader.
	for _, want := range []string{"EDGE-CASE RIGOR", "EXACT output", "STANDARD SEMANTICS", "EDGE CASES", "IDIOMATIC"} {
		if !strings.Contains(implicitAcceptRule, want) {
			t.Errorf("implicitAcceptRule missing %q", want)
		}
	}
	// Must stay benchmark-agnostic: no "hidden automated grader" framing that only holds under a
	// scored harness (the rule is about real-world edge-case rigor, applied identically off-bench).
	for _, banned := range []string{"grade", "graded", "automated check", "NOT shown to you"} {
		if strings.Contains(strings.ToLower(implicitAcceptRule), strings.ToLower(banned)) {
			t.Errorf("implicitAcceptRule contains benchmark-specific framing %q", banned)
		}
	}
}
