package app

import (
	"strings"
	"testing"
)

// Live (large-scale-text-editing, 06:05–06:11): one file took 21 writes across 13 versions, and
// after the first swing note was spent it went L→M→L→M→L in silence — while the byte-identical
// branch beside it spoke on every consecutive repeat. The weaker signal ("you wrote the same bytes
// twice") repeated; the stronger one ("you are cycling between versions") did not.
//
// The once-per-file cap was written when the guard could still stop the turn. It cannot any more,
// so the report is the only channel there is, and a swing nobody mentions is a swing the agent
// never learns about.
func TestEverySwingIsReportedNotJustTheFirst(t *testing.T) {
	g := newRunGuard()
	const path = "/app/apply_macros.vim"

	if w, reg := g.noteEdit(path, "v0", "L"); reg || w != "" {
		t.Fatalf("the first forward edit is not a swing: %q regressed=%v", w, reg)
	}
	if w, reg := g.noteEdit(path, "L", "M"); reg || w != "" {
		t.Fatalf("a second new state is still forward: %q regressed=%v", w, reg)
	}

	first, reg := g.noteEdit(path, "M", "L") // back to a state already held
	if !reg {
		t.Fatal("returning to a state the file already held this turn is a swing")
	}
	if !strings.Contains(first, "restored a content state") {
		t.Errorf("the first note stays the neutral aside a deliberate revert can ignore, got %q", first)
	}

	second, reg := g.noteEdit(path, "L", "M")
	if !reg {
		t.Fatal("the swing back is a swing too")
	}
	if second == "" {
		t.Fatal("a later swing must still be reported — silence here is the defect")
	}
	if !strings.Contains(second, "2 times") {
		t.Errorf("a later note must say how many swings there have been, got %q", second)
	}

	third, _ := g.noteEdit(path, "M", "L")
	if !strings.Contains(third, "3 times") {
		t.Errorf("the count must keep climbing, got %q", third)
	}
	// The cycle's WIDTH is what separates "I reverted one change" from "I am oscillating": here
	// the file has held exactly three states (the pre-turn baseline, L, M).
	if !strings.Contains(third, "3 distinct versions") {
		t.Errorf("the note must say how wide the cycle is, got %q", third)
	}

	// Reporting stays per file: another file's first swing gets its own first-time wording.
	other, reg := g.noteEdit("/app/other.vim", "a", "b")
	if reg || other != "" {
		t.Fatalf("unrelated forward edit: %q regressed=%v", other, reg)
	}
	if w, _ := g.noteEdit("/app/other.vim", "b", "a"); !strings.Contains(w, "restored a content state") {
		t.Errorf("a different file starts its own count, got %q", w)
	}
}

// distinctStates counts states, not writes: a file written back and forth between two versions ten
// times has held two of them (plus the baseline it started from).
func TestDistinctStatesCountsStatesNotWrites(t *testing.T) {
	if n := distinctStates([]uint64{7, 9, 7, 9, 7}); n != 2 {
		t.Errorf("two alternating states = 2, got %d", n)
	}
	if n := distinctStates(nil); n != 0 {
		t.Errorf("no history = 0, got %d", n)
	}
}
