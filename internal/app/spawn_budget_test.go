package app

import (
	"strings"
	"testing"
)

// dispatchBudgetNote tells the agent how many background workers it has launched this turn against
// the cap so it self-limits — delegation exceeds a single agent's context ceiling, but over-spawning
// fragments the context. The note always carries the count; past a soft budget (MaxAgents/4, min 4)
// it turns into a firm "do the rest yourself"; MAGI_SPAWN_BUDGET=0 silences it.
func TestDispatchBudgetNote(t *testing.T) {
	// Under the soft budget: count only, no over-spawn warning.
	n := dispatchBudgetNote(1, 50)
	if !strings.Contains(n, "#1 this turn") || !strings.Contains(n, "of up to 50") {
		t.Errorf("low count should report position and cap: %q", n)
	}
	if strings.Contains(n, "prefer finishing") {
		t.Errorf("under the soft budget must not fire the over-spawn warning: %q", n)
	}
	// At/over the soft budget (50/4≈12): the firm steer-to-solo warning appears.
	if s := dispatchBudgetNote(12, 50); !strings.Contains(s, "SPLIT the context") || !strings.Contains(s, "YOURSELF") {
		t.Errorf("at the soft budget the over-spawn warning must fire: %q", s)
	}
	// Small cap: soft budget floors at 4.
	if s := dispatchBudgetNote(4, 8); !strings.Contains(s, "SPLIT the context") {
		t.Errorf("soft budget floors at 4 workers: %q", s)
	}
	if s := dispatchBudgetNote(3, 8); strings.Contains(s, "SPLIT the context") {
		t.Errorf("below the floor must not warn: %q", s)
	}
	// Disabled → empty.
	t.Setenv("MAGI_SPAWN_BUDGET", "0")
	if s := dispatchBudgetNote(20, 50); s != "" {
		t.Errorf("MAGI_SPAWN_BUDGET=0 must silence the note, got %q", s)
	}
}
