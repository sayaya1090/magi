package app

import (
	"strings"
	"testing"
)

// setState mirrors what the loop does after an edit lands: recordChange updates the file's
// net after-content, which is what the deliverable signature is taken over.
func setState(g *runGuard, path, after string) {
	g.recordChange(path, "", after)
}

// TestTabuFlagsRevisitedFailingState: an exercising command fails at deliverable state A;
// the agent edits to B (clean), then circles back to exactly A — checkTabu must flag that
// revisit, citing the prior failure.
func TestTabuFlagsRevisitedFailingState(t *testing.T) {
	g := newRunGuard()
	// Agent authors state A, then a test fails against it (in production these are separate
	// tool calls; checkTabu is not consulted until the NEXT edit lands).
	setState(g, "sol.py", "vA")
	g.noteExerciseFail("pytest", "AssertionError: expected 2 got 3")
	// Move to a different state → not tabu.
	setState(g, "sol.py", "vB")
	if w := g.checkTabu(); w != "" {
		t.Fatalf("a new (never-failed) state must not warn, got %q", w)
	}
	// Circle back to exactly A → tabu.
	setState(g, "sol.py", "vA")
	w := g.checkTabu()
	if w == "" {
		t.Fatal("returning to a state whose test already failed must be flagged")
	}
	if !strings.Contains(w, "AssertionError") {
		t.Fatalf("the tabu warning should cite the prior failure, got %q", w)
	}
}

// TestTabuWarnsOncePerState: a revisit is flagged once per signature, so a repeated nudge
// can't itself drive more thrashing.
func TestTabuWarnsOncePerState(t *testing.T) {
	g := newRunGuard()
	setState(g, "sol.py", "vA")
	g.noteExerciseFail("python sol.py", "boom")
	setState(g, "sol.py", "vB")
	setState(g, "sol.py", "vA")
	if g.checkTabu() == "" {
		t.Fatal("first revisit should warn")
	}
	if w := g.checkTabu(); w != "" {
		t.Fatalf("second check at the same state must stay quiet, got %q", w)
	}
}

// TestTabuIgnoresInspectOnlyFailure: a failing inspect-only command (a bad ls/grep) is not
// evidence that the DELIVERABLE is bad, so it must not tabu the current state.
func TestTabuIgnoresInspectOnlyFailure(t *testing.T) {
	g := newRunGuard()
	setState(g, "sol.py", "vA")
	g.noteExerciseFail("ls -la nonexistent", "No such file or directory")
	setState(g, "sol.py", "vB")
	setState(g, "sol.py", "vA")
	if w := g.checkTabu(); w != "" {
		t.Fatalf("an inspect-only failure must not tabu the deliverable, got %q", w)
	}
}

// TestTabuNoDeliverableIsNoop: with nothing authored yet, a failure records no tabu and
// checkTabu is quiet (signature 0).
func TestTabuNoDeliverableIsNoop(t *testing.T) {
	g := newRunGuard()
	g.noteExerciseFail("pytest", "collection error")
	if len(g.failedStates) != 0 {
		t.Fatalf("no authored deliverable → nothing to tabu, got %d entries", len(g.failedStates))
	}
	if w := g.checkTabu(); w != "" {
		t.Fatalf("no deliverable → no warning, got %q", w)
	}
}

// TestTabuCleanStateNeverWarns: a state that never failed an exercise is never flagged,
// however many times it is visited.
func TestTabuCleanStateNeverWarns(t *testing.T) {
	g := newRunGuard()
	setState(g, "sol.py", "vA")
	setState(g, "sol.py", "vB")
	setState(g, "sol.py", "vA")
	if w := g.checkTabu(); w != "" {
		t.Fatalf("a never-failed state must not warn, got %q", w)
	}
}

// TestTabuSignatureSpansAllFiles: the tabu state is the whole authored deliverable, not one
// file — a failing {a,b} pair is only re-flagged when BOTH files return to the failing set.
func TestTabuSignatureSpansAllFiles(t *testing.T) {
	g := newRunGuard()
	setState(g, "a.py", "a1")
	setState(g, "b.py", "b1")
	g.noteExerciseFail("pytest", "2 failed")
	// Change only b → different whole-deliverable signature → not tabu.
	setState(g, "b.py", "b2")
	if w := g.checkTabu(); w != "" {
		t.Fatalf("a changed second file makes a new deliverable state, got %q", w)
	}
	// Return b to its failing content → the pair matches the tabu signature again.
	setState(g, "b.py", "b1")
	if g.checkTabu() == "" {
		t.Fatal("both files back to the failing pair must be flagged")
	}
}
