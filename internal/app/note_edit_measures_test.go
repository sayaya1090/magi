package app

import (
	"strings"
	"testing"
)

// "this write left the file byte-for-byte as it already was — nothing changed" is a claim about
// what THIS command did, and the two reads taken around the command are what settles it. It used to
// be decided by comparing the new content against the last state magi had RECORDED — a different
// question, whose answer parts company with the file the moment anything rewrites the path through
// a route magi cannot name a destination for.
//
// Observed live (cobol-modernization, 2026-07-30): the run alternated
//
//	cp /tmp/*_orig.DAT /app/data/*.DAT     (restore the fixtures — a destination magi can name)
//	python3 program.py                     (rewrite them — a destination magi cannot name)
//
// so the record still read ORIG while the files held post-transaction bytes. Every restore moved
// all three files, and every one came back saying nothing had changed. The next command ran the
// COBOL binary over those same files and printed the post-restore result, proving the copy took.
func TestTheNothingChangedNoteIsMeasuredNotRecalled(t *testing.T) {
	const orig, applied = "balance 1180", "balance 0980"
	g := newRunGuard(nil)
	const p = "data/ACCOUNTS.DAT"

	// First restore: the file really moves, and magi records both states.
	if warn, reg := g.noteEdit(p, applied, orig); warn != "" || reg {
		t.Fatalf("a first move forward is silent: warn=%q regressed=%v", warn, reg)
	}

	// …the program rewrites it back, unobserved — magi is told nothing. The restore that follows
	// therefore finds `applied` on disk and leaves `orig`: it changed the file.
	warn, reg := g.noteEdit(p, applied, orig)
	if strings.Contains(warn, "nothing changed") {
		t.Errorf("the command moved the file; magi may not say otherwise:\n%s", warn)
	}
	if !reg || !strings.Contains(warn, "restored a content state") {
		t.Errorf("it returned to a state held earlier, which is what to say:\nwarn=%q regressed=%v", warn, reg)
	}

	// A third swing counts, and the count is over states the file actually held.
	warn, reg = g.noteEdit(p, applied, orig)
	if !reg || !strings.Contains(warn, "already held 2 times") || !strings.Contains(warn, "2 distinct versions") {
		t.Errorf("the swings are counted from the measured states:\nwarn=%q regressed=%v", warn, reg)
	}

	// And the sentence still fires where it is TRUE: a command that restores the fixture and
	// re-applies the program leaves the file exactly as it found it, whatever the record says.
	warn, reg = g.noteEdit(p, applied, applied)
	if !strings.Contains(warn, "byte-for-byte as it already was") {
		t.Errorf("a net-zero command is what the sentence is for:\n%s", warn)
	}
	if reg {
		t.Error("nothing moved, so nothing was reverted")
	}
}

// The ordinary path is unchanged: a file magi has watched the whole way still reads the same.
func TestAnObservedFileReadsTheSameAsBefore(t *testing.T) {
	g := newRunGuard(nil)
	const p = "main.go"
	if warn, _ := g.noteEdit(p, "A", "B"); warn != "" {
		t.Errorf("forward progress is silent: %s", warn)
	}
	if warn, _ := g.noteEdit(p, "B", "B"); !strings.Contains(warn, "nothing changed") {
		t.Errorf("a re-write of identical bytes is named: %q", warn)
	}
	warn, reg := g.noteEdit(p, "B", "A")
	if !reg || !strings.Contains(warn, "restored a content state") {
		t.Errorf("a revert to the baseline is named: warn=%q regressed=%v", warn, reg)
	}
	// Empty on both sides stays silent — absent and empty are not distinguishable from content.
	if warn, reg := g.noteEdit("gone.txt", "", ""); warn != "" || reg {
		t.Errorf("nothing on either side is not a rewrite of anything: warn=%q regressed=%v", warn, reg)
	}
}
