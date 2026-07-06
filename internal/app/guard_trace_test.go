package app

import (
	"encoding/json"
	"fmt"
	"testing"
)

// This file is a RUNTIME TRACE HARNESS (not an assertion suite): it drives runGuard
// exactly as runLoop does — per tool call: check() then, on a mutation, the same
// note* calls execute.go makes; per step: shouldNudge() then stuck() — and logs every
// state transition so the stall/step-budget/re-arm machine can be audited against real
// call sequences. Run: go test ./internal/app -run TestGuardTrace -v

// simCall runs one tool call through the guard the way execute.go does and returns a
// one-line trace of the resulting guard state. kind: "inspect" (no mutation), "edit"
// (file edit → mutated), "bashwrite" (authored a file), "bashexec" (ran a program),
// "bashboth" (authored + ran, e.g. `python x.py > out`). arg varies the fingerprint.
func simCall(g *runGuard, kind, arg string) string {
	raw := json.RawMessage(fmt.Sprintf(`{"a":%q}`, arg))
	block, n, _ := g.check("t_"+kind, raw)
	novel := n == 1
	switch kind {
	case "edit":
		g.mutated(arg, arg) // path=arg, sig=arg → distinct arg = real change
	case "bashwrite":
		g.noteBashWrite("echo x > " + arg)
	case "bashexec":
		g.noteBashExec("python "+arg+".py", novel)
	case "bashboth":
		g.noteBashWrite("python x.py > " + arg)
		g.noteBashExec("python x.py > "+arg, novel)
	}
	return fmt.Sprintf("call kind=%-9s arg=%-4s block=%-5v n=%d | epoch=%d since=%d lastStallAt=%d stallNudges=%d blocked=%d exec=%d progSinceNudge=%v",
		kind, arg, block, n, g.epoch, g.sinceProgress, g.lastStallAt, g.stallNudges, g.blocked, g.execSinceMut, g.progressSinceNudge)
}

// step runs the per-step guard checks (shouldNudge then stuck) and returns their verdicts.
func step(g *runGuard) (nudge, stop string) {
	return g.shouldNudge(), g.stuck()
}

func TestGuardTracePureStallConvergeOn(t *testing.T) {
	g := newRunGuard()
	g.stallConverge = true
	t.Log("SCENARIO: pure no-progress, varied inspect-only calls, 1 call/step, stallConverge ON")
	for i := 0; i < 60; i++ {
		tr := simCall(g, "inspect", fmt.Sprintf("v%d", i)) // distinct arg each time → no repeat block
		nudge, stop := step(g)
		if nudge != "" || stop != "" {
			t.Logf("[step %2d] %s  >>> nudge=%q stop=%q", i, tr, nudge, stop)
		}
		if stop != "" {
			t.Logf("=> FORCE-STOP at step %d (tool call #%d), kind=%q", i, g.calls, stop)
			return
		}
	}
	t.Fatalf("no force-stop within 60 steps (calls=%d) — stall guard did not land", g.calls)
}

func TestGuardTracePureStallConvergeOff(t *testing.T) {
	g := newRunGuard()
	g.stallConverge = false
	t.Log("SCENARIO: pure no-progress, stallConverge OFF (fixed maxStallNudges re-arm)")
	for i := 0; i < 80; i++ {
		tr := simCall(g, "inspect", fmt.Sprintf("v%d", i))
		nudge, stop := step(g)
		if nudge != "" || stop != "" {
			t.Logf("[step %2d] %s  >>> nudge=%q stop=%q", i, tr, nudge, stop)
		}
		if stop != "" {
			t.Logf("=> FORCE-STOP at step %d (tool call #%d), kind=%q", i, g.calls, stop)
			return
		}
	}
	t.Fatalf("no force-stop within 80 steps (calls=%d)", g.calls)
}

func TestGuardTraceEditThenStall(t *testing.T) {
	g := newRunGuard()
	g.stallConverge = true
	t.Log("SCENARIO: one real edit at step 3, then pure no-progress — does the window restart and still land?")
	for i := 0; i < 60; i++ {
		kind, arg := "inspect", fmt.Sprintf("v%d", i)
		if i == 3 {
			kind, arg = "edit", "main.go"
		}
		tr := simCall(g, kind, arg)
		nudge, stop := step(g)
		if nudge != "" || stop != "" || i == 3 {
			t.Logf("[step %2d] %s  >>> nudge=%q stop=%q", i, tr, nudge, stop)
		}
		if stop != "" {
			t.Logf("=> FORCE-STOP at step %d (call #%d), kind=%q epoch=%d (epoch>0 ⇒ clean finish, not error)", i, g.calls, stop, g.epoch)
			return
		}
	}
	t.Fatalf("no force-stop within 60 steps (calls=%d)", g.calls)
}

func TestGuardTraceOscillation(t *testing.T) {
	g := newRunGuard()
	g.stallConverge = true
	t.Log("SCENARIO: implement↔revert oscillation on one file — retractProgress must stop it dodging the stall")
	states := []string{"A", "B"} // A→B→A→B… ; noteEdit sees the revert and we retract
	base := hashContent("")
	_ = base
	for i := 0; i < 80; i++ {
		arg := states[i%2]
		raw := json.RawMessage(fmt.Sprintf(`{"a":%q}`, arg))
		_, _, _ = g.check("t_edit", raw)
		reset := g.mutated("osc.txt", arg)
		// Simulate execute.go's noteEdit + retract: content alternates, so every swing after the
		// first return-to-a-prior-state is a regression.
		_, regressed := g.noteEdit("osc.txt", "", arg) // after=arg; history alternates A/B
		if regressed && reset {
			g.retractProgress()
		}
		nudge, stop := step(g)
		if nudge != "" || stop != "" {
			t.Logf("[step %2d] arg=%s reset=%v regressed=%v | since=%d lastStallAt=%d stallNudges=%d epoch=%d  >>> nudge=%q stop=%q",
				i, arg, reset, regressed, g.sinceProgress, g.lastStallAt, g.stallNudges, g.epoch, nudge, stop)
		}
		if stop != "" {
			t.Logf("=> FORCE-STOP at step %d (call #%d), kind=%q epoch=%d", i, g.calls, stop, g.epoch)
			// O6 regression: with stallConverge ON, a self-reverting oscillation must land on the
			// SAME converged schedule as any other stall (~1 window past the first re-arm), not run
			// the full fixed-re-arm distance. Before the retractProgress fix it stopped at call 50
			// (each swing re-set progressSinceNudge, defeating the collapse); after, it converges by
			// ~26. Bound generously to stay robust, but well under the un-converged 48–50.
			if g.calls > 32 {
				t.Fatalf("oscillation stopped at call %d — stallConverge did NOT collapse (O6 regression: retractProgress must clear progressSinceNudge)", g.calls)
			}
			return
		}
	}
	t.Fatalf("OSCILLATION NEVER STOPPED within 80 steps (calls=%d, epoch=%d) — retractProgress failed to prevent stall-dodge", g.calls, g.epoch)
}

func TestGuardTraceVariedBashWrite(t *testing.T) {
	g := newRunGuard()
	g.stallConverge = true
	t.Log("SCENARIO: agent authors a DIFFERENT junk file every step (echo junkN > fN) — does it ever stall?")
	for i := 0; i < 60; i++ {
		tr := simCall(g, "bashwrite", fmt.Sprintf("f%d", i)) // distinct target each step → mutated true each time
		nudge, stop := step(g)
		if nudge != "" || stop != "" {
			t.Logf("[step %2d] %s  >>> nudge=%q stop=%q", i, tr, nudge, stop)
		}
		if stop != "" {
			t.Logf("=> FORCE-STOP at step %d (call #%d), kind=%q", i, g.calls, stop)
			return
		}
	}
	t.Logf("=> NO force-stop in 60 steps (calls=%d epoch=%d) — varied file-authoring is treated as progress (by design; note for review)", g.calls, g.epoch)
}
