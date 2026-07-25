package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// councilParams applies the shared fallbacks (DefaultMembers / DefaultRule / 3 rounds) when config is
// empty, and passes configured values through unchanged — the single source every council gate uses.
func TestCouncilParams(t *testing.T) {
	// Empty config → shared defaults.
	a := &App{cfg: Config{}}
	m, r, mr := a.councilParams()
	if len(m) != len(council.DefaultMembers()) || len(m) == 0 {
		t.Errorf("empty members must default to DefaultMembers, got %d", len(m))
	}
	if r != council.DefaultRule {
		t.Errorf("empty rule must default to %q, got %q", council.DefaultRule, r)
	}
	if mr != 3 {
		t.Errorf("non-positive maxRounds must default to 3, got %d", mr)
	}

	// Configured values pass through unchanged.
	a2 := &App{cfg: Config{
		CouncilMembers:   []council.Member{{Name: "solo-judge"}},
		CouncilRule:      council.RuleUnanimous,
		CouncilMaxRounds: 5,
	}}
	m2, r2, mr2 := a2.councilParams()
	if len(m2) != 1 || m2[0].Name != "solo-judge" {
		t.Errorf("configured members must pass through, got %+v", m2)
	}
	if r2 != council.RuleUnanimous {
		t.Errorf("configured rule must pass through, got %q", r2)
	}
	if mr2 != 5 {
		t.Errorf("configured maxRounds must pass through, got %d", mr2)
	}
}
