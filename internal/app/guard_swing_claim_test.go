package app

import (
	"strings"
	"testing"
)

// The swing note ends in a claim about the run's own history — "no command ran between some of
// those writes" — and nothing measured it. It was part of the sentence, said every time.
//
// Live: a scratch test file written, RUN, deleted, written again, RUN again (it exited 130),
// deleted. magi told the agent nothing had run between its writes while magi's own exercise
// counter had advanced across every pair.
func TestTheSwingNoteOnlyClaimsWhatItCounted(t *testing.T) {
	const p = "run_test_interrupt.py"
	ran := func(g *runGuard) { g.noteBashExec("python3 "+p, true) }

	g := newRunGuard()
	g.noteEdit(p, "", "v1")                    // create
	ran(g)                                     // …and run it
	g.noteEdit(p, "v1", "")                    // delete → back to the pre-turn state (first swing)
	ran(g)                                     // the agent kept working on the real deliverable in between
	g.noteEdit(p, "", "v2")                    // create again
	ran(g)                                     // …and run it again
	warn, regressed := g.noteEdit(p, "v2", "") // delete again → second swing, the fuller note
	if !regressed || !strings.Contains(warn, "returned to a content state") {
		t.Fatalf("the second swing must produce the counted note, got %q", warn)
	}
	if strings.Contains(warn, "no command ran") {
		t.Errorf("something ran between every pair of writes — magi counted it:\n%s", warn)
	}
	// The rest of the note is unchanged: it still says how often and how wide.
	if !strings.Contains(warn, "2 times this turn") || !strings.Contains(warn, "3 distinct versions") {
		t.Errorf("the measured parts must survive:\n%s", warn)
	}

	// And when it IS true, it is still said — a silent version of this note would hide the case
	// the note exists for: rewriting a file over and over without ever running it.
	g2 := newRunGuard()
	g2.noteEdit(p, "", "v1")
	g2.noteEdit(p, "v1", "")
	g2.noteEdit(p, "", "v2")
	warn2, _ := g2.noteEdit(p, "v2", "")
	if !strings.Contains(warn2, "no command ran between some of those writes") {
		t.Errorf("nothing ran between any of these writes and the note must say so:\n%s", warn2)
	}
}

// Reading a file is not running it: an agent that rewrites and re-reads without ever exercising
// the deliverable is exactly the case the clause describes, so inspection must not silence it.
func TestInspectionDoesNotCountAsRunning(t *testing.T) {
	const p = "run.py"
	g := newRunGuard()
	g.noteEdit(p, "", "v1")
	g.noteBashExec("cat "+p, true) // inspect-only — returns before touching execRuns
	g.noteEdit(p, "v1", "")
	g.noteBashExec("grep -n def "+p, true)
	g.noteEdit(p, "", "v2")
	warn, _ := g.noteEdit(p, "v2", "")
	if !strings.Contains(warn, "no command ran between some of those writes") {
		t.Errorf("cat and grep run nothing; the claim holds:\n%s", warn)
	}
}
