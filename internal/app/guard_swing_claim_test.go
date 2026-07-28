package app

import (
	"strings"
	"testing"
)

// The swing note ends where the counting ends. It used to add "no command ran between some of those
// writes" — first asserted unconditionally, then measured from an exercise counter fed by a verb
// table deciding which commands "run" something. A sentence whose truth rests on a hand-maintained
// list of verbs is a guess wearing a measurement's clothes, and this one was an accusation.
//
// What survives is counted: how often the file came back to a state it already held, and how many
// versions it has moved among.
func TestTheSwingNoteSaysOnlyWhatItCounted(t *testing.T) {
	const p = "run_test_interrupt.py"
	g := newRunGuard()
	g.noteEdit(p, "", "v1")
	g.noteEdit(p, "v1", "")
	g.noteEdit(p, "", "v2")
	warn, regressed := g.noteEdit(p, "v2", "")
	if !regressed || !strings.Contains(warn, "returned to a content state") {
		t.Fatalf("the second swing must produce the counted note, got %q", warn)
	}
	if !strings.Contains(warn, "2 times this turn") || !strings.Contains(warn, "3 distinct versions") {
		t.Errorf("the measured parts are the note:\n%s", warn)
	}
	if strings.Contains(warn, "no command ran") {
		t.Errorf("a claim about what ran is not something this note counted:\n%s", warn)
	}
}
