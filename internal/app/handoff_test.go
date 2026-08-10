package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
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
	// The probe's real cadence is half a minute — right for dialling a neighbour, impossible to
	// wait out in a test. Only the clock is changed; every rule under test is the shipped one.
	was := handoffProbe
	handoffProbe = 60 * time.Millisecond
	t.Cleanup(func() { handoffProbe = was })
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

// Silence is not one thing, and the wait has to say which.
//
// A transcript that stopped growing may be a peer inside a ten-minute build, one blocked on a
// permission prompt nobody is at the keyboard for, or a daemon killed with the turn half done. All
// three look identical from the log, which is all the waiting side can read — so without a probe
// the wait runs its full two hours and then reports "not finished", which is true of every one of
// them and useful for none.
func TestAPeerThatWillNeverAnswerIsReportedRatherThanWaitedOut(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("theirs", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/them"}))
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))

	if err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design", Session: "theirs", Request: "name the tokens",
		Probe: func() (string, bool) {
			return "design's daemon stopped answering with the work unfinished", true
		},
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the death to be reported", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "stopped answering") {
					return true
				}
			}
		}
		return false
	})
	// Over means over: it is no longer outstanding, so the finish gate stops holding a turn open
	// for an answer that cannot arrive.
	waitFor(t, "the pending record to clear", func() bool {
		return len(f.a.PendingHandoffs("mine")) == 0
	})
}

// Blocked is news, not an ending.
//
// A companion waiting on a permission prompt can still be answered — by a person, who has to be
// told, and the asker is the only thing in a position to tell one. So it is passed on and the wait
// continues, and the piece stays outstanding because it still is.
func TestABlockedPeerIsReportedAndStillWaitedFor(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("theirs", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/them"}))
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))

	var probes int
	if err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design", Session: "theirs", Request: "name the tokens",
		Probe: func() (string, bool) {
			probes++
			return "design is blocked waiting for a person: permission for bash", false
		},
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the block to be reported", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "blocked waiting for a person") {
					return true
				}
			}
		}
		return false
	})
	// Still out: the answer may yet come, and a cleared record would tell the finish gate this
	// turn has everything it asked for.
	if out := f.a.PendingHandoffs("mine"); len(out) != 1 {
		t.Errorf("a blocked peer's work is no longer outstanding: %+v", out)
	}
	// Said once. The same state reported every half minute is a conversation filling with one fact.
	before := countContaining(f.myMessages(), "blocked waiting for a person")
	waitFor(t, "a second probe", func() bool { return probes >= 2 })
	if after := countContaining(f.myMessages(), "blocked waiting for a person"); after != before {
		t.Errorf("one stuck state was reported %d times, was %d", after, before)
	}
}

func countContaining(msgs []session.Message, want string) int {
	n := 0
	for _, m := range msgs {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, want) {
				n++
			}
		}
	}
	return n
}

// An answer from a machine whose log is not on this disk is fetched, not read.
//
// The whole local design rests on never being told anything: the answer is written where every
// answer is written and the wait goes and reads it. That stops at the machine boundary, and the
// substitution has to happen HERE — a wait that fell back to reading a local session for a remote
// hand-off would either find nothing forever or, if a session id happened to collide, deliver
// somebody else's words as the answer.
func TestAnAnswerFromAnotherMachineIsAskedForRatherThanRead(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))

	var mu sync.Mutex
	finished := false
	// A session id this store has never heard of: their log is on their disk, and nothing here
	// should be trying to read it.
	err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design on buildbox", Session: "s_over_there", Request: "name the tokens",
		Answer: func() (string, bool) {
			mu.Lock()
			defer mu.Unlock()
			if !finished {
				return "", false
			}
			return "surface-container-low, and the label is on-surface-variant.", true
		},
	})
	if err != nil {
		t.Fatalf("Expect refused work whose transcript is elsewhere: %v", err)
	}
	time.Sleep(handoffPoll + 500*time.Millisecond)
	for _, m := range f.myMessages() {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "answered") {
				t.Fatal("an answer arrived before the far side had one")
			}
		}
	}
	mu.Lock()
	finished = true
	mu.Unlock()

	waitFor(t, "the fetched answer to arrive", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "surface-container-low") {
					return true
				}
			}
		}
		return false
	})
}

// A push wakes the wait, instead of the wait having to reach its next tick.
//
// The clock here is three seconds, which is right for reading a log file on this disk and is what
// a hand-off across a machine used to spawn a process for. Now the far daemon holds a connection
// and says when something happens — and if the news then sat here until a tick, the wait would be
// pushed to and poll anyway.
//
// A deliberately long poll makes the difference observable: with the nudge ignored, nothing can
// arrive inside it.
func TestAPushWakesTheWaitWithoutWaitingForItsClock(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))

	was, wasProbe := handoffPoll, handoffProbe
	handoffPoll, handoffProbe = time.Hour, time.Hour
	t.Cleanup(func() { handoffPoll, handoffProbe = was, wasProbe })

	var mu sync.Mutex
	finished := false
	ready := make(chan struct{}, 1)
	err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design on buildbox", Session: "rcpt-9", Request: "name the tokens",
		Answer: func() (string, bool) {
			mu.Lock()
			defer mu.Unlock()
			if !finished {
				return "", false
			}
			return "surface-container-low, and the label is on-surface-variant.", true
		},
		Ready: ready,
	})
	if err != nil {
		t.Fatalf("Expect refused work whose transcript is elsewhere: %v", err)
	}
	mu.Lock()
	finished = true
	mu.Unlock()
	ready <- struct{}{}

	waitFor(t, "the pushed answer to arrive without a tick to carry it", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "surface-container-low") {
					return true
				}
			}
		}
		return false
	})
}

// The wait lets go of whatever was holding the answer open, however it ended.
//
// Across a machine that is a process and a connection per outstanding hand-off. The wait is the
// only thing that knows it has stopped listening — by an answer arriving, by its deadline, or by
// this daemon going away — so it is the only thing that can say so.
func TestTheWaitReleasesWhatWasHoldingTheAnswerOpen(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))

	released := make(chan struct{})
	err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design on buildbox", Session: "rcpt-9", Request: "name the tokens",
		Answer: func() (string, bool) { return "all done", true },
		Done:   func() { close(released) },
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the wait ended and left a connection held open on two machines")
	}
}

// A finishing turn is asked what the answer it got was worth.
//
// It is the only reader in a position to say. Whether an answer arrived is known here already;
// whether it was the answer needed is a judgement about content, and it needs the question, the
// answer, and the work they were both for — which is what a turn has and a later reader does not.
func TestAFinishingTurnIsAskedWhatTheAnswerWasWorth(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design on buildbox", Session: "rcpt-1", Request: "name the tokens",
		Answer: func() (string, bool) { return "surface-container-low", true },
	})
	if err != nil {
		t.Fatal(err)
	}
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

	got := f.a.takeAnsweredHandoffs("mine")
	if len(got) != 1 || got[0].Who != "design on buildbox" {
		t.Fatalf("an answer arrived and nothing knows it is waiting to be judged: %+v", got)
	}
	// Emptied as it is read: the question is asked once. A nag that repeats until it gets a
	// verdict would collect verdicts written to make it stop.
	if again := f.a.takeAnsweredHandoffs("mine"); len(again) != 0 {
		t.Errorf("it would be asked a second time: %+v", again)
	}
}

// A companion the turn has already judged is not asked about.
//
// The turn's own tool calls are the evidence. Told to do what it just did, an agent learns that
// the thing asking is not reading what it does.
func TestSomebodyAlreadyJudgedIsNotAskedAbout(t *testing.T) {
	call := func(who string) event.Event {
		args, _ := json.Marshal(map[string]string{"who": who, "verdict": "good", "why": "it landed"})
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m1", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				CallID: "c1", Name: "rate_handoff", Args: args}}})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	rated := ratedThisTurn([]event.Event{call("Design"), call("builder")})
	if !rated["design"] || !rated["builder"] {
		t.Fatalf("the turn's own verdicts were not read off its calls: %+v", rated)
	}
	if rated["scribe"] {
		t.Error("somebody who was never rated came back as rated")
	}
	// A turn that rated nobody leaves nothing behind.
	if n := len(ratedThisTurn(nil)); n != 0 {
		t.Errorf("%d verdicts found in a turn with no calls", n)
	}
}

// The answer arrives beside the form it was asked to take.
//
// Whether it came back is known already. Whether it is the THING is a comparison, and this is the
// one moment making it is cheap: the question, the shape asked for, and what arrived are all in
// front of the reader at once. Delivered without it, an answer that filled none of the headings
// reads exactly like one that filled them all.
func TestTheDeliveredAnswerCarriesTheFormItWasAskedFor(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design on buildbox", Session: "rcpt-1", Request: "name the tokens",
		AnswerAs: "- surface:\n- on-surface:",
		Answer:   func() (string, bool) { return "surface: surface-container-low", true },
	})
	if err != nil {
		t.Fatal(err)
	}
	var note string
	waitFor(t, "the answer to arrive", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "surface-container-low") {
					note = p.Text
					return true
				}
			}
		}
		return false
	})
	for _, want := range []string{"answer in this form", "- on-surface:", "Check it against the form"} {
		if !strings.Contains(note, want) {
			t.Errorf("the delivered answer does not carry %q:\n%s", want, note)
		}
	}
}

// A hand-off with no form delivers the way it always did.
//
// Nothing about the note should mention a form that was never asked for — an instruction to check
// against something that does not exist is the shape of a description naming a way to do something
// there is no way to do.
func TestAnAnswerWithNoFormIsDeliveredPlainly(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	err := f.a.Expect("mine", event.Actor{Kind: event.ActorAgent, ID: "agent"}, port.Elsewhere{
		Who: "design", Session: "rcpt-2", Request: "name the tokens",
		Answer: func() (string, bool) { return "surface-container-low", true },
	})
	if err != nil {
		t.Fatal(err)
	}
	var note string
	waitFor(t, "the answer to arrive", func() bool {
		for _, m := range f.myMessages() {
			for _, p := range m.Parts {
				if strings.Contains(p.Text, "surface-container-low") {
					note = p.Text
					return true
				}
			}
		}
		return false
	})
	if strings.Contains(note, "form") {
		t.Errorf("it tells the reader to check against a form nobody asked for:\n%s", note)
	}
	if !strings.Contains(note, "Fold it into what you have") {
		t.Errorf("the plain delivery lost its closing line:\n%s", note)
	}
}

// The gate that asks for a tool is the gate that permits it.
//
// A turn that has declared itself finished drops its tool calls, so a prompt asking for one is a
// prompt asking for nothing unless the same gate says the tool may run. Live, it did not: the
// agent called rate_handoff, there was no result, and no record was written. Asking and permitting
// are set together here so they cannot come apart again.
func TestTheGateThatAsksForAToolAlsoPermitsIt(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	f.a.mu.Lock()
	st := f.a.stateLocked("mine")
	st.answered = append(st.answered, answeredHandoff{Who: "design", Request: "name the tokens"})
	f.a.mu.Unlock()

	var ts turnState
	tc := turnCtx{s: session.Session{ID: "mine"}}
	act, done := f.a.askWhatTheAnswersWereWorth(context.Background(), tc, nil, &ts)
	if !done || act != loopContinue {
		t.Fatalf("the gate did not keep the turn open to be answered: %v %v", act, done)
	}
	if !ts.finishTools["rate_handoff"] {
		t.Error("it asked for a tool the declared turn will throw away")
	}
	// And the question is in the conversation, naming who is waiting on a verdict.
	found := false
	for _, m := range f.myMessages() {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "rate_handoff") && strings.Contains(p.Text, "design") {
				found = true
			}
		}
	}
	if !found {
		t.Error("nothing was asked")
	}
}

// After declaring finished: a tool the finish path asked for runs, a re-ask runs and reopens the
// turn, everything else is dropped and said so.
//
// The rule is that a declared turn does no more work, and it stays. What was wrong was the silence
// — the agent called something, nothing happened, and the transcript kept the call with no result,
// which is what a call that DID happen looks like to whoever reads it next.
func TestWhatADeclaredTurnMayStillDo(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	call := func(n string) *session.ToolCall { return &session.ToolCall{Name: n} }
	ctx := context.Background()

	var ts turnState
	ts.declared = true
	ts.allowAtFinish("rate_handoff")
	kept := f.a.callsAfterDeclaring(ctx, "mine", []*session.ToolCall{
		call("bash"), call("rate_handoff"), call("write")}, &ts)
	if len(kept) != 1 || kept[0].Name != "rate_handoff" {
		t.Fatalf("kept %v, want only what the finish path asked for", calledNames(kept))
	}
	if !ts.declared {
		t.Error("an ordinary call reopened a finished turn")
	}
	if len(ts.dropped) != 2 {
		t.Errorf("it dropped %v in silence", ts.dropped)
	}
	// And says so, keeping the turn open long enough to be told.
	act, done := f.a.sayWhatWasNotRun(ctx, turnCtx{s: session.Session{ID: "mine"}}, &ts)
	if !done || act != loopContinue {
		t.Fatalf("the drop was not reported: %v %v", act, done)
	}
	told := lastMessage(t, f)
	for _, want := range []string{"`bash`", "`write`", "NOT finished after all"} {
		if !strings.Contains(told, want) {
			t.Errorf("what it was told does not carry %q:\n%s", want, told)
		}
	}
	// Asked once, never again — the same turn must not be nagged about it every step.
	if _, again := f.a.sayWhatWasNotRun(ctx, turnCtx{s: session.Session{ID: "mine"}}, &ts); again {
		t.Error("it said the same thing twice")
	}
}

// A re-ask is an assertion that the work is not done, so it reopens the turn — and only twice.
//
// Allowed while the turn stayed closed it would be pointless: the answer arrives after the turn
// has ended and lands where nothing reads it. Unbounded it would be a turn that never finishes.
func TestAReAskReopensTheTurnAndOnlyTwice(t *testing.T) {
	f := newHandoffFixture(t)
	f.append("mine", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w/me"}))
	call := func(n string) *session.ToolCall { return &session.ToolCall{Name: n} }
	ctx := context.Background()

	var ts turnState
	for i := 1; i <= maxReasks; i++ {
		ts.declared = true
		kept := f.a.callsAfterDeclaring(ctx, "mine", []*session.ToolCall{call("hand_off")}, &ts)
		if len(kept) != 1 {
			t.Fatalf("re-ask %d was thrown away", i)
		}
		if ts.declared {
			t.Fatalf("re-ask %d left the turn closed, so the answer would land in a dead turn", i)
		}
		if told := lastMessage(t, f); !strings.Contains(told, "NOT finished any more") {
			t.Errorf("re-ask %d was allowed without saying the turn had reopened: %s", i, told)
		}
	}
	// Past the cap it is dropped like anything else.
	ts.declared = true
	if kept := f.a.callsAfterDeclaring(ctx, "mine", []*session.ToolCall{call("hand_off")}, &ts); len(kept) != 0 {
		t.Error("a third re-ask was allowed, so the turn need never end")
	}
	if !ts.declared {
		t.Error("a refused re-ask reopened the turn anyway")
	}
}

func calledNames(cs []*session.ToolCall) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func lastMessage(t *testing.T, f *handoffFixture) string {
	t.Helper()
	msgs := f.myMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		for j := len(msgs[i].Parts) - 1; j >= 0; j-- {
			if s := msgs[i].Parts[j].Text; strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}
