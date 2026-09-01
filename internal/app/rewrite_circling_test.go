package app

import (
	"fmt"
	"strings"
	"testing"
)

// A file that keeps taking NEW content is the one shape no revert check can see.
//
// The guard had two things to say about a write: it changed nothing (byte-identical), or it put the
// file back where it had already been (a swing). Neither covers writing something genuinely new,
// over and over, without ever arriving — and that is what a model does when it cannot verify its
// own work. Measured on a live run: 42 writes to one path across twenty-five minutes, every one a
// different text, the model itself writing "I apologize for the prolonged iteration" five times
// word for word. Nothing in magi said anything, and the run ended because a person killed it.
func TestAFileThatKeepsTakingNewContentIsRemarkedOn(t *testing.T) {
	g := newRunGuard(nil)
	said := map[int]string{}
	prev := "v0"
	for i := 1; i <= 20; i++ {
		next := fmt.Sprintf("v%d", i)
		warn, regressed := g.noteEdit("haiku.txt", prev, next)
		if regressed {
			t.Fatalf("write %d: every content here is new — nothing was reverted", i)
		}
		if warn != "" {
			said[i] = warn
		}
		prev = next
	}

	if len(said) == 0 {
		t.Fatal("twenty rewrites of one file in one turn and the guard never said a word")
	}
	// It waits: ordinary work edits a file a few times, and a guard that speaks at three is noise.
	for i := 1; i < rewriteNoteAt; i++ {
		if w, ok := said[i]; ok {
			t.Errorf("write %d is ordinary iteration and must pass in silence, got %q", i, w)
		}
	}
	if _, ok := said[rewriteNoteAt]; !ok {
		t.Errorf("the %dth rewrite is where it speaks; it did not", rewriteNoteAt)
	}
	// Then it keeps count rather than saying it once and going quiet for the next thirty.
	if _, ok := said[rewriteNoteAt+rewriteNoteEvery]; !ok {
		t.Errorf("it must say it again at %d — one aside does not describe forty minutes",
			rewriteNoteAt+rewriteNoteEvery)
	}
	if _, ok := said[rewriteNoteAt+1]; ok {
		t.Error("every write is not the same as every few — that is noise, not a report")
	}
	// The number is the whole content of the report, so it has to be the real one.
	if w := said[rewriteNoteAt]; !strings.Contains(w, fmt.Sprintf("%d different contents", rewriteNoteAt)) {
		t.Errorf("the note must say how many, got %q", w)
	}
}

// It has to survive the file being changed by something other than the write it is watching.
//
// magi's own harness runs gofmt after every successful write to a .go file, so the bytes on disk
// routinely differ from what the guard last recorded, and noteEdit folds that difference in as its
// own history entry. That makes the history grow by TWO in one call — and a cadence expressed as a
// modulo can step straight over its window and never land in it again. Measured on the first
// version of this guard: fourteen calls across twenty-seven states, not one note. Silence like that
// is worse than having no guard, because it reads as an all-clear.
func TestTheNoteSurvivesSomethingElseTouchingTheFile(t *testing.T) {
	g := newRunGuard(nil)
	notes := 0
	for i := 1; i <= 20; i++ {
		// What the write produced, and what the file actually held by the time the next write
		// read it — a formatter having been over it in between.
		wrote := fmt.Sprintf("v%d", i)
		reformatted := fmt.Sprintf("v%d formatted", i)
		if warn, _ := g.noteEdit("main.go", reformatted, wrote); warn != "" {
			notes++
		}
		// The next call's `before` is the formatter's version, not what the write left.
		_ = reformatted
	}
	if notes == 0 {
		t.Error("twenty rewrites with a formatter in between and the guard never spoke — " +
			"the case this exists for is the case it went silent in")
	}
}

// The number it reports has to be the number of different contents, not the size of its own
// bookkeeping. A turn that reverts twice has not written two more versions.
func TestTheCountIsDistinctContentsAndNotHistoryEntries(t *testing.T) {
	g := newRunGuard(nil)
	var last string
	say := func(before, after string) string {
		w, _ := g.noteEdit("f.txt", before, after)
		last = after
		return w
	}
	// Eight distinct contents with two reverts in the middle: v1..v6, back to v2, on to v7, v8.
	for _, v := range []string{"v1", "v2", "v3", "v4", "v5", "v6"} {
		say(last, v)
	}
	say("v6", "v2") // a revert — a content the file already held
	say("v2", "v7")
	warn := say("v7", "v8")
	// Written: v1..v8 = eight distinct contents. The history has more entries than that.
	if warn == "" {
		t.Fatal("eight different contents is where it speaks")
	}
	if !strings.Contains(warn, "8 different contents") {
		t.Errorf("the revert is not a ninth content; the note must say 8, got %q", warn)
	}
}

// And it must not fire on a file that is merely edited a few times, which is most turns.
func TestOrdinaryIterationIsNotRemarkedOn(t *testing.T) {
	g := newRunGuard(nil)
	prev := ""
	for i := 1; i <= rewriteNoteAt-1; i++ {
		next := fmt.Sprintf("draft %d", i)
		if warn, _ := g.noteEdit("notes.md", prev, next); warn != "" {
			t.Errorf("edit %d of a file is ordinary work; the guard said %q", i, warn)
		}
		prev = next
	}
}

// The two reports stay apart: a swing is still a swing and still withholds progress credit, and
// this new one is not a swing and must not pretend to be.
func TestCirclingAndSwingingAreDifferentReports(t *testing.T) {
	g := newRunGuard(nil)
	// Ten distinct versions gets the circling note, and never claims a regression.
	prev := "a"
	for i := 0; i < 10; i++ {
		next := fmt.Sprintf("v%d", i)
		if _, regressed := g.noteEdit("f.txt", prev, next); regressed {
			t.Fatalf("new content at step %d is not a regression", i)
		}
		prev = next
	}
	// Now put it back to a state it already held: that IS a swing, and says so.
	warn, regressed := g.noteEdit("f.txt", prev, "v0")
	if !regressed {
		t.Fatal("returning to an earlier content state is a swing")
	}
	if !strings.Contains(warn, "restored a content state") {
		t.Errorf("a swing keeps its own words, got %q", warn)
	}
}
