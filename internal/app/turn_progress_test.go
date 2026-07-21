package app

import "testing"

func TestTurnProgressCheckDefault(t *testing.T) {
	if turnProgressCheckEnabled() {
		t.Fatal("default must be OFF")
	}
	t.Setenv("MAGI_TURN_PROGRESS_CHECK", "1")
	if !turnProgressCheckEnabled() {
		t.Error("=1 must enable")
	}
}

// The step-based rabbit-hole check: with the flag on, stepsSinceMut reaching the stall threshold
// makes stuck() return "idle" (routes to recovery); off, it never fires. A mutation resets it.
func TestGuardIdleStuck(t *testing.T) {
	// Flag OFF → never idle, however many steps.
	g := newRunGuard()
	for i := 0; i < progressStallSteps+5; i++ {
		g.noteStep()
	}
	if g.stuck() == "idle" {
		t.Fatal("idle must not fire with the flag OFF")
	}

	t.Setenv("MAGI_TURN_PROGRESS_CHECK", "1")
	g2 := newRunGuard()
	for i := 0; i < progressStallSteps-1; i++ {
		g2.noteStep()
	}
	if g2.stuck() == "idle" {
		t.Fatal("idle must not fire below the stall threshold")
	}
	g2.noteStep() // reaches the threshold
	if g2.stuck() != "idle" {
		t.Fatalf("idle must fire at the stall threshold (stepsSinceMut=%d)", g2.stepsSinceMut)
	}
	// A real mutation resets the step counter → no longer idle.
	g2.mutated("/app/out.c", "sig1")
	if g2.stuck() == "idle" {
		t.Fatal("a mutation must reset the rabbit-hole counter")
	}
}

// The one-shot nudge fires once at the nudge threshold, not again until a mutation re-arms it.
func TestGuardIdleNudge(t *testing.T) {
	t.Setenv("MAGI_TURN_PROGRESS_CHECK", "1")
	g := newRunGuard()
	for i := 0; i < progressNudgeSteps-1; i++ {
		g.noteStep()
		if g.idleNudgeDue() {
			t.Fatalf("nudge fired too early at step %d", i)
		}
	}
	g.noteStep() // nudge threshold
	if !g.idleNudgeDue() {
		t.Fatal("nudge must fire at the nudge threshold")
	}
	g.noteStep()
	if g.idleNudgeDue() {
		t.Fatal("nudge must be one-shot per window")
	}
	// A mutation re-arms it.
	g.mutated("/app/x", "s")
	for i := 0; i < progressNudgeSteps; i++ {
		g.noteStep()
	}
	if !g.idleNudgeDue() {
		t.Fatal("a mutation must re-arm the nudge")
	}
}
