package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// When a meeting closes, MeetingTurn forgets the room so a later turn for the same meeting (a
// reopen) prepares a FRESH participant session instead of reusing the one that was just reaped.
// Without forget, roomFor keeps handing back a session id whose state was dropped, and the viewer
// draws an empty working pane beside a sentence that plainly took some.
func TestForgettingARoomMakesAReopenPrepareAfresh(t *testing.T) {
	h := handover{rooms: newSideSessions()}
	const meeting = "m-42"

	h.remember(meeting, session.SessionID("child-1"))
	if got := h.roomFor(meeting); got != "child-1" {
		t.Fatalf("roomFor did not return the remembered session: %q", got)
	}

	h.forget(meeting)
	if got := h.roomFor(meeting); got != "" {
		t.Errorf("the room was not forgotten — a reopen would reuse the reaped session %q", got)
	}

	// A reopen remembers a new one, and they are independent.
	h.remember(meeting, session.SessionID("child-2"))
	if got := h.roomFor(meeting); got != "child-2" {
		t.Errorf("a reopened room did not take the fresh session: %q", got)
	}
}

// forget must be safe when there is no rooms map (a handover built without one) and for a meeting
// that was never remembered — a double close cannot panic.
func TestForgettingARoomIsSafeWhenAbsent(t *testing.T) {
	(handover{}).forget("never") // nil rooms — must not panic

	h := handover{rooms: newSideSessions()}
	h.forget("never-remembered") // absent key — harmless
	if got := h.roomFor("never-remembered"); got != "" {
		t.Errorf("an absent room resolved to %q", got)
	}
}
