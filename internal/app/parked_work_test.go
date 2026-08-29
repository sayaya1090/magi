package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// park puts interjections in a session's queue without going through enqueueInterject, which also
// writes a deferral ledger entry and so needs a store and a bus. What is under test is the two
// readers, and what they read is this field.
func park(a *App, sid session.SessionID, texts ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	for i, t := range texts {
		st.pendingInterject = append(st.pendingInterject, pendingInterjection{MsgID: string(sid) + "_" + string(rune('a'+i)), Text: t})
	}
}

// TestWhatThePersonTypedIsWaitingInEverySessionAtOnce.
//
// Both readers walk the whole process, not one session, because the question they answer is asked
// by the fleet: is anything of the PERSON'S already waiting anywhere. A reader that stopped at the
// first session it found would say no while the thing you typed sits in another one's queue — and
// it would say it intermittently, since which session it looked at first is map order.
func TestWhatThePersonTypedIsWaitingInEverySessionAtOnce(t *testing.T) {
	a := &App{}
	park(a, "s_quiet")       // a session with a queue and nothing in it
	a.mu.Lock()              // a session whose state was freed under it
	a.stateLocked("s_gone")  // materialise the entry…
	a.states["s_gone"] = nil // …then leave it the way a reader can find it
	a.mu.Unlock()            //
	park(a, "s_one", "check the parser")
	park(a, "s_two", "and the lexer", "then land it")

	if !a.PersonWaiting() {
		t.Error("three sessions, three parked messages, and nobody is reported waiting")
	}
	got := map[string]string{}
	for _, p := range a.ParkedWork() {
		got[string(p.Session)+": "+p.Text] = p.Text
	}
	for _, want := range []string{
		"s_one: check the parser",
		"s_two: and the lexer",
		"s_two: then land it",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("%q is waiting and was not reported; got %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("three messages are parked, %d reported: %v", len(got), got)
	}
}

// TestAnIdleFleetReportsNothingWaitingUntilOneMessageDoes: the empty answer has to be the empty
// answer, because the screen draws a badge on it — a reader that counted a session with an empty
// queue would put "1 waiting" on a companion nobody has typed at. And the threshold is one: a
// single message is the whole of what the person is waiting on.
//
// The freed session belongs here rather than in the test above. PersonWaiting returns at the first
// session it finds something in, so with anything queued it may never reach the freed entry — which
// session it looks at first is map order, and a guard held only when the map cooperates is held on
// some runs and not others.
func TestAnIdleFleetReportsNothingWaitingUntilOneMessageDoes(t *testing.T) {
	a := &App{}
	park(a, "s_quiet") // a session with a queue and nothing in it
	a.mu.Lock()
	a.stateLocked("s_gone")  // materialise a session…
	a.states["s_gone"] = nil // …then free it the way a reader can find it
	a.mu.Unlock()

	if a.PersonWaiting() {
		t.Error("nothing is parked and something is reported waiting")
	}
	if got := a.ParkedWork(); len(got) != 0 {
		t.Errorf("nothing is parked and %d item(s) came back: %v", len(got), got)
	}

	park(a, "s_quiet", "one thing")
	if !a.PersonWaiting() {
		t.Error("one parked message is somebody waiting")
	}
}
