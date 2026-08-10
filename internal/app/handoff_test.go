package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// handoffFixture is an App over a real store, with a second session standing in for the companion
// the work was handed to. A real store because the whole mechanism is "read their log" — a fake one
// would be a test of the fake.
type handoffFixture struct {
	a     *App
	store *jsonl.Store
	t     *testing.T
}

func newHandoffFixture(t *testing.T) *handoffFixture {
	t.Helper()
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &handoffFixture{
		a:     closeAfter(t, New(st, nil, builtin.NewRegistry(), bus.New(), nil, Config{})),
		store: st,
		t:     t,
	}
}

func (f *handoffFixture) append(sid string, evs ...event.Event) {
	f.t.Helper()
	if _, err := f.store.Append(context.Background(), session.SessionID(sid), evs...); err != nil {
		f.t.Fatal(err)
	}
}

func ev(t *testing.T, typ event.Type, d any) event.Event {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: typ, Data: b, TS: time.Now()}
}

// said is one assistant message, which is where an answer lives.
func said(t *testing.T, text string) event.Event {
	return ev(t, event.TypePartAppended, event.PartAppendedData{
		MessageID: "m_" + text[:4], Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: text}})
}

// waitFor polls until cond or the deadline; the mechanism under test is itself a poll.
func waitFor(t *testing.T, why string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", why)
}

func (f *handoffFixture) myMessages() []session.Message {
	f.t.Helper()
	msgs, _, err := f.a.SessionState(context.Background(), "mine")
	if err != nil {
		f.t.Fatal(err)
	}
	return msgs
}

// The answer comes back into the asker's own conversation.
//
// This is the whole feature. Handing work over already crossed the socket; what did not exist was
// the way back, and its absence left an agent two bad choices — stop and poll a screen it cannot
// see, or carry on and lose the work. The answer has to arrive where the running turn will read it,
// which is the conversation, because the agent re-reads that at every step.
func TestAHandedOffAnswerComesBackIntoTheAskersConversation(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("theirs", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/them"}))
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))

	err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design", Session: "theirs", Request: "name the tokens for the empty state"})
	if err != nil {
		t.Fatalf("Expect: %v", err)
	}
	// Nothing yet: they have not finished, and a note before then would be a conclusion invented
	// out of whatever they happened to be saying.
	time.Sleep(handoffPoll + 500*time.Millisecond)
	for _, m := range f.myMessages() {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "design answered") {
				t.Fatal("an answer was delivered before they finished")
			}
		}
	}

	f.append("theirs",
		said(t, "surface-container-low, and the label is on-surface-variant."),
		ev(t, event.TypeTurnFinished, event.TurnFinishedData{}))

	waitFor(t, "the answer to arrive", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "surface-container-low") {
					return true
				}
			}
		}
		return false
	})

	// It says who answered and WHAT IT ANSWERS. A turn that handed out three pieces gets three
	// replies in whatever order the work finished, and without the question quoted back it has
	// three answers and no way to pair them up.
	note := ""
	for _, m := range f.myMessages() {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "surface-container-low") {
				note = p.Text
			}
		}
	}
	for _, want := range []string{"design", "name the tokens for the empty state"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not carry %q:\n%s", want, note)
		}
	}
	// And it is no longer outstanding, or the finish gate would go on reporting a piece that came
	// back.
	if out := f.a.PendingHandoffs("mine"); len(out) != 0 {
		t.Errorf("still pending after the answer landed: %+v", out)
	}
}

// The answer is the turn that finishes AFTER the work was handed over.
//
// Their log is not empty when a request arrives — they have been working all day — so a watch that
// took "the last thing they said" would deliver the answer to somebody else's question immediately,
// and it would look exactly like a real answer.
func TestTheAnswerIsTheTurnAfterTheRequestNotTheOneBefore(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("theirs",
		ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/them"}),
		said(t, "yesterday's answer about the button ripple"),
		ev(t, event.TypeTurnFinished, event.TurnFinishedData{}))
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))

	if err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design", Session: "theirs", Request: "the empty state"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(handoffPoll + 500*time.Millisecond)
	for _, m := range f.myMessages() {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "ripple") {
				t.Fatalf("a finished turn from BEFORE the request was delivered as its answer:\n%s", p.Text)
			}
		}
	}

	f.append("theirs", said(t, "today's answer about the empty state"),
		ev(t, event.TypeTurnFinished, event.TurnFinishedData{}))
	waitFor(t, "the real answer", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "today's answer") {
					return true
				}
			}
		}
		return false
	})
}

// A finishing turn is told once that a piece is still out.
//
// Not held: handing work over is asynchronous so the asker can carry on, and blocking here would
// make it the synchronous call it exists to avoid. But finishing with a piece outstanding throws
// that piece away — the watch delivers into a turn that has ended, where nothing reads it — and
// only the agent can weigh whether the answer was load-bearing.
func TestAFinishingTurnIsToldWhatIsStillOut(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("theirs", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/them"}))
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	if err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design", Session: "theirs", Request: "name the tokens"}); err != nil {
		t.Fatal(err)
	}

	tc := turnCtx{s: session.Session{ID: "mine"}, depth: 0}
	ts := &turnState{}
	act, done := f.a.noteOutstandingHandoffs(context.Background(), tc, ts)
	if !done || act != loopContinue {
		t.Fatalf("a turn finishing with work out was allowed to close (done=%v act=%v)", done, act)
	}
	note := ""
	for _, m := range f.myMessages() {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "has not come back") {
				note = p.Text
			}
		}
	}
	if note == "" {
		t.Fatal("no note was written")
	}
	for _, want := range []string{"design", "name the tokens", "do not need to ask again"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note is missing %q:\n%s", want, note)
		}
	}
	// Once. Said at every step it would be the loop it is meant to prevent.
	if _, again := f.a.noteOutstandingHandoffs(context.Background(), tc, ts); again {
		t.Error("the same turn was told twice")
	}
}

// And a turn with nothing out closes without a word.
func TestATurnWithNothingOutIsNotStopped(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	_, done := f.a.noteOutstandingHandoffs(context.Background(),
		turnCtx{s: session.Session{ID: "mine"}, depth: 0}, &turnState{})
	if done {
		t.Error("a turn that handed out nothing was stopped anyway")
	}
}
