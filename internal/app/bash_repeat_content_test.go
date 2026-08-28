package app

import "testing"

// Whether a bash command WROTE something and whether its TEXT repeated are two different
// questions, and only the first decides if there is content worth comparing. The content check
// used to be gated on the second.
//
// mutated() compares the COMMAND TEXT on the bash path — every bash mutation shares one slot — so
// re-running a byte-identical write command returns reset=false even when the file changed
// underneath it in between. Observed live (large-scale-text-editing, 2026-07-30):
// `cat > apply_macros.vim <<'ENDOFFILE' …` was noted at one call, a write tool changed the file at
// the next, and the SAME heredoc command after that was passed over in silence — so the state it
// left never reached contentHist and the count magi states as fact ("returned to a state it
// already held N times, among M distinct versions") ran behind the truth from then on.
func TestARepeatedBashWriteCommandStillGetsItsContentRead(t *testing.T) {
	g := newRunGuard(nil)
	cmd := "cat > f.vim << 'EOF'\nA\nEOF\n"

	authored, reset := g.noteBashWrite(cmd)
	if !authored || !reset {
		t.Fatalf("first run of a write command authors and bumps: authored=%v reset=%v", authored, reset)
	}

	// Another tool changes the same file. It lands under its own key (the path), so the bash
	// slot still holds the old command text.
	if !g.mutated("f.vim", `{"path":"f.vim","content":"B"}`) {
		t.Fatal("a write tool call to a different signature is a real mutation")
	}

	// The identical command again. It DID write — that is what the caller needs read — but the
	// signature has not moved, so reset is false. The two answers must not be conflated.
	authored, reset = g.noteBashWrite(cmd)
	if !authored {
		t.Error("the command still authors a file, so its content is still worth comparing")
	}
	if reset {
		t.Error("the signature did not move, so no bump was made and none may be retracted")
	}
}

// The two gates in noteToolOutcome now read the question each of them is actually about:
// the comparison runs on `authored`, the retraction on `reset`. This pins the second half —
// retractProgress must not take back a bump that was never made, or it steals the window from an
// earlier, real mutation.
func TestRetractionNeedsABumpToTakeBack(t *testing.T) {
	g := newRunGuard(nil)
	// Climb the window a little, then make a real mutation that resets it.
	for i := 0; i < 5; i++ {
		g.check("bash", nil)
	}
	if !g.mutated("real.go", "sig-1") {
		t.Fatal("a first mutation resets")
	}
	for i := 0; i < 3; i++ {
		g.check("bash", nil)
	}
	before := g.sinceProgress

	// A retraction with no bump behind it would restore the pre-reset climb, undoing a window
	// that a genuine mutation had earned.
	g.retractProgress()
	if got := g.sinceProgress; got < before {
		t.Errorf("a retraction never shortens the window: %d → %d", before, got)
	}
}
