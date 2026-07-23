package app

import "testing"

// applyEdit replays the execute.go wiring for one file edit: mutate (bumps the epoch, zeroes the
// progress windows), then run the self-regression check, and — exactly as the loop does — retract
// the progress reset when the edit returned the file to a state it already held this turn.
func applyEdit(g *runGuard, path, before, after string) {
	reset := g.mutated(path, "write:"+after) // sig varies with content, so a real change bumps the epoch
	_, regressed := g.noteEdit(path, before, after)
	if regressed && reset {
		g.retractProgress()
	}
}

// An oscillating rewrite loop (A→B→A→B…) is churn, not progress: the step-based idle counter
// (stepsSinceMut, which drives stuck()=="idle") must keep climbing across the swings so a
// reasoning-heavy rabbit hole trips the idle recovery instead of running to the wall clock.
// Regression guard for the custom-memory-heap-crash timeout: retractProgress restored the
// tool-call counter but NOT stepsSinceMut, so every oscillating rewrite refilled the idle window.
func TestOscillatingEditsClimbIdleCounter(t *testing.T) {
	g := newRunGuard()
	stateA, stateB := "int x = 1;", "int x = 2;"
	cur := stateA
	applyEdit(g, "user.cpp", "", cur) // first author: forward progress, not a regression

	// Alternate A↔B, taking loop steps between edits, the way a reasoning loop does.
	for i := 0; i < progressStallSteps+4; i++ {
		g.noteStep()
		next := stateA
		if cur == stateA {
			next = stateB
		}
		applyEdit(g, "user.cpp", cur, next)
		cur = next
	}

	if g.stuck() != "idle" {
		t.Fatalf("an oscillating rewrite loop must eventually trip stuck()==idle, got %q (stepsSinceMut=%d)",
			g.stuck(), g.stepsSinceMut)
	}
}

// The counterpart: genuine forward edits (each a new, never-seen state) are progress, so mutated()
// resets the idle window and it never trips — a task making real edits is not a rabbit hole.
func TestForwardEditsResetIdleCounter(t *testing.T) {
	g := newRunGuard()
	prev := ""
	for i := 0; i < progressStallSteps+4; i++ {
		g.noteStep()
		next := "line " + string(rune('a'+i%26)) + string(rune('0'+i)) // distinct every time
		applyEdit(g, "prog.c", prev, next)
		prev = next
	}
	if g.stuck() == "idle" {
		t.Fatalf("forward edits (new state each time) must keep resetting the idle window, but stuck()==idle "+
			"(stepsSinceMut=%d)", g.stepsSinceMut)
	}
}
