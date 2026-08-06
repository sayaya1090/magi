package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// What a daemon says it is blocked on has to become a prompt on an attached screen.
//
// The two halves were each covered and their JOIN was not: the daemon's side is tested against a
// socket, the modal is tested against an event a test wrote by hand, and nothing checked that the
// event the daemon's side produces is the event the modal reads. A field named differently at
// either end would show a prompt with a blank command, or no prompt at all, and both halves would
// still be green.
//
// So this drives the real Model with the real conversion: daemon.Waiting → Event → the screen.
func TestADaemonsPendingPermissionBecomesAPromptOnScreen(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "clean the tree")

	w := &daemon.Waiting{
		ID: "call_7", Kind: "permission", What: "bash",
		Args:   json.RawMessage(`{"command":"rm -rf build"}`),
		Reason: "destructive command detected",
		Since:  "2026-08-07T00:00:00Z",
	}
	ev, err := w.Event(s.m.sid)
	if err != nil {
		t.Fatal(err)
	}
	s.send(eventMsg{ev: ev, sid: s.m.sid, sub: s.m.mainSub})

	if s.m.perm == nil {
		t.Fatal("the daemon's pending permission drew no prompt — an attached terminal would sit " +
			"in front of a run that had simply stopped")
	}
	if s.m.perm.callID != "call_7" {
		t.Errorf("the prompt is keyed %q; an answer would go nowhere", s.m.perm.callID)
	}
	view := stripANSI(s.view())
	for _, want := range []string{"bash", "rm -rf build", "destructive command detected"} {
		if !strings.Contains(view, want) {
			t.Errorf("the prompt does not show %q — the decision is made on less than the daemon sent:\n%s",
				want, view)
		}
	}
}

// The same for a question, whose options ARE the answer: a modal with no choices in it is one
// nobody can reply to.
func TestADaemonsPendingQuestionBecomesAPromptOnScreen(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "port the handler")

	w := &daemon.Waiting{
		ID: "q1#1", Kind: "question", What: "which branch should this land on?",
		Options: []string{"main", "release-2"}, Since: "2026-08-07T00:00:00Z",
	}
	ev, err := w.Event(s.m.sid)
	if err != nil {
		t.Fatal(err)
	}
	s.send(eventMsg{ev: ev, sid: s.m.sid, sub: s.m.mainSub})

	if s.m.quest == nil {
		t.Fatal("the daemon's pending question drew no prompt")
	}
	if s.m.quest.callID != "q1#1" {
		t.Errorf("the question is keyed %q", s.m.quest.callID)
	}
	view := stripANSI(s.view())
	for _, want := range []string{"which branch should this land on?", "main", "release-2"} {
		if !strings.Contains(view, want) {
			t.Errorf("the question does not show %q:\n%s", want, view)
		}
	}
}
