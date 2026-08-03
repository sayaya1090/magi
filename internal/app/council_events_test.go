package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// councilParams applies the shared fallbacks (DefaultMembers / DefaultRule) when config is empty,
// and passes configured values through unchanged.
func TestCouncilParams(t *testing.T) {
	// Empty config → shared defaults.
	a := &App{cfg: Config{}}
	m, r := a.councilParams()
	if len(m) != len(council.DefaultMembers()) || len(m) == 0 {
		t.Errorf("empty members must default to DefaultMembers, got %d", len(m))
	}
	if r != council.DefaultRule {
		t.Errorf("empty rule must default to %q, got %q", council.DefaultRule, r)
	}

	// Configured values pass through unchanged.
	a2 := &App{cfg: Config{
		CouncilMembers: []council.Member{{Name: "solo-judge"}},
		CouncilRule:    council.RuleUnanimous,
	}}
	m2, r2 := a2.councilParams()
	if len(m2) != 1 || m2[0].Name != "solo-judge" {
		t.Errorf("configured members must pass through, got %+v", m2)
	}
	if r2 != council.RuleUnanimous {
		t.Errorf("configured rule must pass through, got %q", r2)
	}
}
