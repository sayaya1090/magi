package app

import "testing"

// noteExerciseResult counts a FAILING exercising command only when the mutation epoch has advanced
// since that command's last counted failure — the agent edited the deliverable and the SAME build/
// test still fails. A pass of a command clears its own count; a passing SIBLING command must not
// reset a different failing one (the whole point of keying by command, not deliverable state).
func TestExerciseChurnPerCommand(t *testing.T) {
	g := newRunGuard()
	rel := "g++ -O2 -o release main.cpp && ./release" // the failing target
	dbg := "g++ -O0 -g -o debug main.cpp && ./debug"  // a passing sibling

	// A failure before any deliverable edit (epoch 0) is not deliverable churn.
	if n := g.noteExerciseResult(rel, true); n != 0 {
		t.Fatalf("fail before any edit must not count, got %d", n)
	}

	// Each edit bumps the epoch; the release command failing after each edit is one churn cycle,
	// and the debug command PASSING in between must not reset the release count.
	for want := 1; want <= 4; want++ {
		g.mutated("user.cpp", sig(want)) // epoch -> want
		if n := g.noteExerciseResult(dbg, false); n != 0 {
			t.Fatalf("a passing command always returns 0, got %d", n)
		}
		if n := g.noteExerciseResult(rel, true); n != want {
			t.Fatalf("after edit %d, release churn = %d, want %d (debug passing must not reset it)", want, n, want)
		}
	}
	if g.exerciseChurnMax() != 4 {
		t.Fatalf("exerciseChurnMax = %d, want 4", g.exerciseChurnMax())
	}

	// A repeat failure with NO new edit (epoch unchanged) must not increment.
	if n := g.noteExerciseResult(rel, true); n != 4 {
		t.Fatalf("repeat fail with no edit must not increment, got %d want 4", n)
	}

	// The release command finally PASSING clears its own count → convergence.
	if n := g.noteExerciseResult(rel, false); n != 0 {
		t.Fatalf("a pass must clear the command's count, got %d", n)
	}
	if g.exerciseChurnMax() != 0 {
		t.Fatalf("after the failing command passes, max churn must be 0, got %d", g.exerciseChurnMax())
	}
}

// Whitespace-only differences normalize to the same key; inspect-only commands never count.
func TestExerciseChurnNormalizeAndInspect(t *testing.T) {
	g := newRunGuard()
	g.mutated("user.cpp", sig(1))
	if n := g.noteExerciseResult("make   test", true); n != 1 { // collapsed spacing
		t.Fatalf("first fail = %d, want 1", n)
	}
	g.mutated("user.cpp", sig(2))
	if n := g.noteExerciseResult("make test", true); n != 2 { // same key modulo whitespace
		t.Fatalf("whitespace variant must share the key, got %d want 2", n)
	}
	// Inspect-only commands (ls/grep/cat) are not exercising the deliverable and never count.
	if n := g.noteExerciseResult("ls -la", true); n != 0 {
		t.Fatalf("inspect-only must not count, got %d", n)
	}
}

// The cap flag: unset uses the default, a positive integer overrides, 0/garbage/negative disables.
func TestExerciseChurnCapFlag(t *testing.T) {
	t.Setenv("MAGI_EXERCISE_CHURN_CAP", "")
	if got := exerciseChurnCap(); got != defaultExerciseChurnCap {
		t.Errorf("unset must use default %d, got %d", defaultExerciseChurnCap, got)
	}
	if !exerciseChurnLandEnabled() {
		t.Error("default must be enabled")
	}
	t.Setenv("MAGI_EXERCISE_CHURN_CAP", "9")
	if got := exerciseChurnCap(); got != 9 {
		t.Errorf("override to 9, got %d", got)
	}
	t.Setenv("MAGI_EXERCISE_CHURN_CAP", "0")
	if got := exerciseChurnCap(); got != 0 || exerciseChurnLandEnabled() {
		t.Errorf("0 must disable, got cap=%d enabled=%v", got, exerciseChurnLandEnabled())
	}
	t.Setenv("MAGI_EXERCISE_CHURN_CAP", "nonsense")
	if got := exerciseChurnCap(); got != 0 {
		t.Errorf("garbage must disable, got %d", got)
	}
	t.Setenv("MAGI_EXERCISE_CHURN_CAP", "-2")
	if got := exerciseChurnCap(); got != 0 {
		t.Errorf("negative must disable, got %d", got)
	}
}
