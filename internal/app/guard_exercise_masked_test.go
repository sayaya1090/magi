package app

import (
	"testing"
)

// The check-churn counter exists to notice "the same build, N edits later, still the same
// failure" — and it is fed by the bash tool's exit code. A command whose exit belongs to a
// trailing `echo`/`tail` therefore reports every failing build as a PASS and resets the counter,
// which is how a build that failed over and over never registered as churning at all. Only an
// exit that is really the command's own counts as convergence.
func TestMaskedExitIsNotConvergence(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  string
		want bool
	}{
		{"plain build", "cd /app && make world", true},
		{"plain test", "pytest -q", true},
		{"test with a redirect but no mask", "go test ./... > /tmp/t.log 2>&1", true},
		// The live form: the exit is the trailing echo's, so it proves nothing either way.
		{"build ; echo exit", `make world > /tmp/build.log 2>&1; echo "exit=$?" >> /tmp/build.log`, false},
		{"build ; echo ; tail", `make world > /tmp/b.log 2>&1; echo "exit=$?" >> /tmp/b.log; tail -30 /tmp/b.log`, false},
		{"build | tail", "make world 2>&1 | tail -100", false},
		{"build || true", "make world || true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exerciseConverged(tc.cmd); got != tc.want {
				t.Errorf("exerciseConverged(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// Whether an exit code belongs to the trailing `echo` is a fact about the command text, so it may
// not depend on what the model said the call was FOR. The notes the model reads are gated on
// verify — that is what keeps them quiet on benign `mkdir x; echo done` calls — but this judgement
// feeds magi's own counter, and gating it there let one optional field switch the counter off:
// observed live as a build submitted with verify=false whose echo's exit 0 was booked as the build
// converging.
func TestMaskingIsJudgedFromTheCommandNotTheDeclaredIntent(t *testing.T) {
	const masked = `make -j 4 > /tmp/build1.log 2>&1; echo "build exit=$?" >> /tmp/build1.log`
	if exerciseConverged(masked) {
		t.Error("a `;`-masked build is masked whether or not the call declared itself a verification")
	}
	if !exerciseConverged("make -j 4") {
		t.Error("the same build without the tail is the command's own exit")
	}
}

// End of the same story on a live guard: a build that has been failing across edits keeps its
// climbed count when the agent re-runs it under a masking tail, so the landing gate still sees
// the churn instead of being told the build converged.
func TestMaskedExitDoesNotClearClimbedChurn(t *testing.T) {
	g := newRunGuard()
	const build = `make world > /tmp/build.log 2>&1; echo "exit=$?" >> /tmp/build.log`

	for want := 1; want <= 3; want++ {
		g.mutated("runtime/shared_heap.c", sig(want))
		if n := g.noteExerciseResult(build, true); n != want {
			t.Fatalf("after edit %d, churn = %d, want %d", want, n, want)
		}
	}
	// The masked exit-0 result the tool actually returns for this command reaches the seam as a
	// success — and must be refused as convergence evidence.
	if exerciseConverged(build) {
		t.Fatal("a `;`-masked build must not be treated as a converged exercise")
	}
	if g.exerciseChurnMax() != 3 {
		t.Fatalf("churn was cleared by a masked exit: max = %d, want 3", g.exerciseChurnMax())
	}
	// An honest run of the same build still converges it — the count clears only on real evidence.
	if !exerciseConverged("cd /app/ocaml && make world") {
		t.Fatal("an unmasked build must count as convergence evidence")
	}
	if n := g.noteExerciseResult(build, false); n != 0 || g.exerciseChurnMax() != 0 {
		t.Fatalf("a real pass must clear the count, got %d / max %d", n, g.exerciseChurnMax())
	}
}
