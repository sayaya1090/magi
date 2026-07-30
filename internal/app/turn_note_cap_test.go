package app

import (
	"strings"
	"testing"
)

// A turn note is a promise: remember{scope:"turn"} answers "magi will hand this back before the
// turn ends". The queue is bounded, and past the bound the note was dropped — while the tool went
// on answering that it had been kept. An agent told it has a reminder waiting stops writing the
// fact down anywhere else, so a false "noted" is worse than a refusal: it costs the agent the note
// AND the habit of recording it.
func TestATurnNoteThatWasNotKeptSaysSo(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})

	for i := 0; i < turnNotesCap; i++ {
		if err := a.noteForTurn(sid, strings.Repeat("x", i+1)); err != nil {
			t.Fatalf("note %d is inside the bound: %v", i, err)
		}
	}
	// The bound is reported, and the reason names what to do instead.
	err := a.noteForTurn(sid, "the heap grows from the LEFT — do not re-derive this")
	if err == nil {
		t.Fatal("the note past the cap was discarded, so the caller must not be told it was kept")
	}
	for _, want := range []string{"NOT kept", "limit", "scope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in the reason:\n%s", want, err)
		}
	}
	// And it really is absent from what gets handed back — the error is not cosmetic.
	if block := a.turnNotesBlock(sid); strings.Contains(block, "grows from the LEFT") {
		t.Error("the reason said the note was dropped; the finish seam must agree")
	}

	// Inside the bound, the two silent-return paths are still successes: the note IS queued, so
	// "noted" is true for both the first write and an identical repeat of it.
	b, sid2, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	if err := b.noteForTurn(sid2, "port 8080 is taken by a stale server"); err != nil {
		t.Fatalf("first note: %v", err)
	}
	if err := b.noteForTurn(sid2, "port 8080 is taken by a stale server"); err != nil {
		t.Fatalf("the same note twice is one note, and it is queued: %v", err)
	}
	if n := strings.Count(b.turnNotesBlock(sid2), "port 8080"); n != 1 {
		t.Errorf("a duplicate is folded, not doubled: %d copies", n)
	}
	// An empty note has nothing to hand back, and saying so beats a silent success.
	if err := b.noteForTurn(sid2, "   "); err == nil {
		t.Error("an empty note is not a kept note")
	}
}
