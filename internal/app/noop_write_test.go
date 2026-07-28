package app

import (
	"encoding/json"
	"testing"
)

// A write whose bytes are identical to what the file already holds reports "wrote N bytes" — the
// same sentence a real change produces. Measured live (large-scale-text-editing): the same 215
// bytes written to the same path nine times across five minutes, then the same 225 bytes eight
// more times across thirteen, 17 of 24 writes changing nothing, with no signal of any kind.
//
// The note states the observation and stops there: nothing moved, which is a fact, and whether
// that was intended is the agent's to say.
func TestANoOpWriteSaysSo(t *testing.T) {
	g := newRunGuard()
	body := "call setreg('a', \"dd\")\n"

	if warn, regressed := g.noteEdit("apply.vim", "", body); warn != "" || regressed {
		t.Fatalf("the first real write must pass silently: warn=%q regressed=%v", warn, regressed)
	}
	warn, regressed := g.noteEdit("apply.vim", body, body)
	if warn == "" {
		t.Fatal("a byte-identical rewrite must be reported, not swallowed")
	}
	if regressed {
		t.Error("nothing moved either way — an identical rewrite is not a regression")
	}
	// It keeps saying so: the run that produced this wrote the same bytes nine times, and a note
	// that fires once would have left eight of them silent.
	if again, _ := g.noteEdit("apply.vim", body, body); again == "" {
		t.Error("every no-op write must say so — the loop this exists for is a repeated one")
	}
	// And it does not swallow the next REAL change.
	if w, _ := g.noteEdit("apply.vim", body, body+"call setreg('b', \"x\")\n"); w != "" {
		t.Errorf("a real change after a no-op must stay silent, got %q", w)
	}
}

// A file first touched by a write that matches what is already on disk is the same fact, and the
// baseline must be recorded so a later real edit is still seen as forward motion rather than as a
// return to a state nobody registered.
func TestANoOpWriteOnAFileNotYetSeenStillRegistersItsBaseline(t *testing.T) {
	g := newRunGuard()
	same := "already here\n"
	if warn, _ := g.noteEdit("f.txt", same, same); warn == "" {
		t.Fatal("a write matching the file's existing bytes must be reported on first touch too")
	}
	if warn, regressed := g.noteEdit("f.txt", same, "changed\n"); warn != "" || regressed {
		t.Errorf("the following real change must read as forward motion: warn=%q regressed=%v", warn, regressed)
	}
	// Returning to the baseline is a regression, which only holds if that baseline was recorded.
	if _, regressed := g.noteEdit("f.txt", "changed\n", same); !regressed {
		t.Error("going back to the state the file started in must still read as a revert")
	}
}

// The live loop this came from: identical writes separated by a scratch redirect. Each redirect is
// a genuine bash mutation and bumps the SHARED epoch, which used to hand the next identical write a
// fresh repeat count — so nine of them in five minutes never approached the block that check's own
// documentation promises for "replaying the identical write".
func TestAScratchRedirectDoesNotRearmAnIdenticalWrite(t *testing.T) {
	g := newRunGuard()
	w := jsonRaw(`{"path":"apply.vim","content":"call setreg('a', \"dd\")"}`)
	sig := canonicalArgs(w)

	blockedAt := 0
	for i := 1; i <= repeatLimit+4; i++ {
		block, _, _ := g.check("write", w)
		if block && blockedAt == 0 {
			blockedAt = i
			break
		}
		g.mutated("apply.vim", sig) // identical content → no real change
		// …and between two writes, the agent stages a scratch file. A real mutation, a real epoch
		// bump, and nothing whatever to do with apply.vim.
		g.noteBashWrite("head -1 /app/input.csv > /tmp/i" + string(rune('0'+i)) + ".txt")
	}
	if blockedAt == 0 {
		t.Fatalf("an identical write repeated %d times was never blocked", repeatLimit+4)
	}
	// One real write, then repeatLimit replays of it, then the block: the first call carries the
	// path's pre-mutation state and so counts on its own, which is right — it was a real change.
	if blockedAt != repeatLimit+2 {
		t.Errorf("blocked at call %d; want %d (1 real write + %d replays)", blockedAt, repeatLimit+2, repeatLimit)
	}
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }
