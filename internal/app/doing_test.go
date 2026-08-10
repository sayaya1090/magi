package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// What a running tool reports has to survive being asked for by another process.
//
// A progress note rides a transient event: published on this engine's bus and never written to the
// log. Everything watching from outside — a browser console, an attached terminal, a fleet listing
// — reads the log or asks over a socket, so without a kept copy the answer to "what is it on" is
// silence, and a turn making steady progress is indistinguishable from one wedged on a dead socket.
func TestARunningToolsNoteIsReadableFromOutside(t *testing.T) {
	a := &App{}
	sid := session.SessionID("s_1")

	if got, ok := a.Doing(sid); ok || got != "" {
		t.Fatalf("a session nothing has reported on says %q (ok=%v)", got, ok)
	}

	a.noteDoing(sid, "call_1", "check 6, 4m12s elapsed, still running")
	got, ok := a.Doing(sid)
	if !ok || got != "check 6, 4m12s elapsed, still running" {
		t.Fatalf("Doing = %q (ok=%v)", got, ok)
	}

	// The latest wins. A note is a heartbeat, not a history: keeping the first would leave the
	// screen saying "check 1" for the whole of a twenty-minute wait.
	a.noteDoing(sid, "call_1", "check 7, 4m42s elapsed, still running")
	if got, _ := a.Doing(sid); got != "check 7, 4m42s elapsed, still running" {
		t.Errorf("a later note did not replace the earlier one: %q", got)
	}
}

// The note goes away when the call that was reporting it returns.
//
// It says what is happening NOW. Left behind, it becomes a claim about work that finished — and
// the worst version of that is a listing showing "still running" beside a companion that has been
// idle since yesterday.
func TestTheNoteEndsWithTheCallThatMadeIt(t *testing.T) {
	a := &App{}
	sid := session.SessionID("s_1")
	a.noteDoing(sid, "call_1", "still running")

	// Some other call finishing is not this one's news.
	a.clearDoing(sid, "call_2")
	if got, ok := a.Doing(sid); !ok || got != "still running" {
		t.Fatalf("an unrelated call's result blanked the note: %q (ok=%v)", got, ok)
	}

	a.clearDoing(sid, "call_1")
	if got, ok := a.Doing(sid); ok || got != "" {
		t.Fatalf("the note outlived its call: %q (ok=%v)", got, ok)
	}
}

// And it does not cross a turn boundary.
//
// The clear above needs the call id, and three of the places that report progress have none — a
// compaction, a stalled stream, a loop nudge. Those are cleared by the turn ending, which is the
// coarser rule that catches everything: nothing said during the last request is news about this one.
func TestANewTurnStartsWithNothingBeingWaitedOn(t *testing.T) {
	a := &App{}
	sid := session.SessionID("s_1")
	a.noteDoing(sid, "", "context too large for the model — compacting and retrying")

	a.resetForNewTopLevel(sid)
	if got, ok := a.Doing(sid); ok || got != "" {
		t.Fatalf("a new turn inherited the last one's note: %q (ok=%v)", got, ok)
	}
}

// An empty note is not a note. wait_for builds its text by formatting, and a formatting that came
// out blank would otherwise replace a real note with nothing — a line that vanishes mid-wait and
// reads as "it stopped".
func TestABlankNoteDoesNotEraseARealOne(t *testing.T) {
	a := &App{}
	sid := session.SessionID("s_1")
	a.noteDoing(sid, "call_1", "still running")
	a.noteDoing(sid, "call_1", "   ")
	if got, ok := a.Doing(sid); !ok || got != "still running" {
		t.Fatalf("a blank note erased a real one: %q (ok=%v)", got, ok)
	}
}
