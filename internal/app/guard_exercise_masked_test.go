package app

import (
	"encoding/json"
	"testing"
)

// The check-churn counter exists to notice "the same build, N edits later, still the same
// failure" — and it is fed by the bash tool's exit code. A command whose exit belongs to a
// trailing `echo`/`tail` therefore reports every failing build as a PASS and resets the counter,
// which is how a build that failed 12 times in a row never registered as churning at all. Only an
// exit that is really the command's own counts as convergence.
func TestMaskedExitIsNotConvergence(t *testing.T) {
	verify := json.RawMessage(`true`)
	for _, tc := range []struct {
		name   string
		cmd    string
		verify json.RawMessage
		want   bool
	}{
		{"plain build", "cd /app && make world", verify, true},
		{"plain test", "pytest -q", verify, true},
		{"test with no verify flag", "go test ./...", nil, true},
		// The live form: the exit is the trailing echo's, so it proves nothing either way.
		{"build ; echo exit", `make world > /tmp/build.log 2>&1; echo "exit=$?" >> /tmp/build.log`, verify, false},
		{"build ; echo ; tail", `make world > /tmp/b.log 2>&1; echo "exit=$?" >> /tmp/b.log; tail -30 /tmp/b.log`, verify, false},
		{"build | tail", "make world 2>&1 | tail -100", verify, false},
		// A `|| …` mask needs no declared intent — it can only ever hide the exit code.
		{"build || true", "make world || true", nil, false},
		// Weak models send booleans as strings; that must not silently disable the judgement.
		{"quoted verify flag", "make world 2>&1 | tail -100", json.RawMessage(`"true"`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exerciseConverged(tc.cmd, tc.verify); got != tc.want {
				t.Errorf("exerciseConverged(%q, verify=%s) = %v, want %v", tc.cmd, tc.verify, got, tc.want)
			}
		})
	}
}

// End of the same story on a live guard: a build that has been failing across edits keeps its
// climbed count when the agent re-runs it under a masking tail, so the landing gate still sees
// the churn instead of being told the build converged.
func TestMaskedExitDoesNotClearClimbedChurn(t *testing.T) {
	g := newRunGuard()
	const build = `make world > /tmp/build.log 2>&1; echo "exit=$?" >> /tmp/build.log`
	verify := json.RawMessage(`true`)

	for want := 1; want <= 3; want++ {
		g.mutated("runtime/shared_heap.c", sig(want))
		if n := g.noteExerciseResult(build, true); n != want {
			t.Fatalf("after edit %d, churn = %d, want %d", want, n, want)
		}
	}
	// The masked exit-0 result the tool actually returns for this command reaches the seam as a
	// success — and must be refused as convergence evidence.
	if exerciseConverged(build, verify) {
		t.Fatal("a `;`-masked build must not be treated as a converged exercise")
	}
	if g.exerciseChurnMax() != 3 {
		t.Fatalf("churn was cleared by a masked exit: max = %d, want 3", g.exerciseChurnMax())
	}
	// An honest run of the same build still converges it — the count clears only on real evidence.
	if !exerciseConverged("cd /app/ocaml && make world", verify) {
		t.Fatal("an unmasked build must count as convergence evidence")
	}
	if n := g.noteExerciseResult(build, false); n != 0 || g.exerciseChurnMax() != 0 {
		t.Fatalf("a real pass must clear the count, got %d / max %d", n, g.exerciseChurnMax())
	}
}
