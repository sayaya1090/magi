package app

import "testing"

// After a "repeat"-kind stuck recovery lands, resetRepeat must give the parent a FULLY fresh window —
// the same as resetStall PLUS blocked=0. If it clears only the blocked/stall counters and leaves the
// step-based rabbit-hole counter (stepsSinceMut) high, stuck() re-trips as "idle" the instant the
// parent resumes and force-stops it, silently undoing a successful recovery (the recovery child's
// mutations landed in ITS session, so mutated() never zeroed this guard).
func TestResetRepeatClearsIdleCounter(t *testing.T) {
	t.Setenv("MAGI_TURN_PROGRESS_CHECK", "1") // idle detection on (default)
	g := newRunGuard()
	g.blocked = blockedBudget            // stuck() would return "repeat"…
	g.stepsSinceMut = progressStallSteps // …and "idle" the moment repeat is cleared

	if k := g.stuck(); k != "repeat" {
		t.Fatalf("precondition: want repeat, got %q", k)
	}
	g.resetRepeat()
	if g.stepsSinceMut != 0 {
		t.Errorf("resetRepeat must reset the step-based idle counter, got %d", g.stepsSinceMut)
	}
	if k := g.stuck(); k != "" {
		t.Fatalf("after a repeat recovery the guard must be clear (no immediate idle re-halt), got %q", k)
	}
}
