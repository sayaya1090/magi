package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// answeredIDs is every message the log says was answered where it waited.
func answeredIDs(t *testing.T, a *App, sid session.SessionID) map[string]bool {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range evs {
		if e.Type != event.TypeInterjectionAnswered {
			continue
		}
		var d event.InterjectionAnsweredData
		if json.Unmarshal(e.Data, &d) == nil && d.MessageID != "" {
			out[d.MessageID] = true
		}
	}
	return out
}

// A message answered where it waited has to SAY it was, or the screen goes on showing it as
// untouched.
//
// The queue lives in this process's memory and no client can see it, so leaving the queue is not an
// event anybody outside can observe. The one thing a client reads is this fact, and the terminal
// keys real behaviour off it: the waiting glyph on the bubble, and the hoist that pins every still-
// waiting bubble to the tail. Without the fact the model answers the message and its bubble stays
// parked at the bottom, below the reply, wearing the mark that means nobody has got to it.
//
// Two of the four paths out of the queue said it and two did not, and the two that did not are the
// two that ANSWER — which is the only case where the omission is visible to a person.
func TestAnsweringWhereItWaitedIsSaidOnTheRecord(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	a.enqueueInterject(ctx, sid, "m_1", "how many files did you touch?")
	if got := answeredIDs(t, a, sid); got["m_1"] {
		t.Fatal("nothing has answered it yet")
	}

	a.sayAnsweredInline(ctx, sid, "m_1")

	if got := answeredIDs(t, a, sid); !got["m_1"] {
		t.Error("a message answered where it waited must be on the record as answered — " +
			"a client has no other way to learn its bubble stopped waiting")
	}
	// An empty id is not a fact about anything; saying it would mark a bubble nobody named.
	before := len(answeredIDs(t, a, sid))
	a.sayAnsweredInline(ctx, sid, "")
	if after := len(answeredIDs(t, a, sid)); after != before {
		t.Error("an empty id must say nothing at all")
	}
}

// The same thing, driven through a real path rather than the helper.
//
// A test that only calls sayAnsweredInline proves the helper works and nothing about whether anyone
// calls it — which is the exact shape the defect had: the event existed, two paths emitted it, and
// the two that answer did not. So this one queues a message, starts a turn over it, and lets the
// triage answer it the way it answers a question: plain text, no route. What the record says
// afterwards is the whole test.
func TestTheTurnStartTriageSaysSoWhenItAnswersOne(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.enqueueInterject(ctx, sid, "m_1", "how many files did you touch?")
	// Only an entry a finish boundary has already passed is re-decided at the next turn's start.
	a.markBoundarySeen(sid)

	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, agent: AgentSpec{Name: "main"}, depth: 0}
	// fakeLLM's default reply is plain text and no tool call — an answer, not a route.
	a.reviewWaitingAtTurnStart(ctx, tc, "count the rows")

	if !answeredIDs(t, a, sid)["m_1"] {
		t.Error("the triage answered it where it waited and said nothing on the record — " +
			"the bubble keeps its waiting glyph, pinned under the answer")
	}
	if a.hasPendingInterject(sid) {
		t.Error("answered where it waited: it should have left the queue")
	}
}

// And the other site that answers: the drain at the finish boundary.
//
// A whole turn runs with a message already waiting. The turn-start review leaves it alone — only an
// entry a boundary has already passed is re-decided there — so the boundary's own drain is what
// picks it up, and the triage answers it inline from the session's context. The record has to say
// so for the same reason as above: this is the moment the person watches the answer appear with the
// question still parked below it.
func TestTheBoundaryDrainSaysSoWhenItAnswersOne(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.enqueueInterject(ctx, sid, "m_1", "how many files did you touch?")

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "count the rows"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	waitForTerminal(t, a, sid)

	// The boundary drain runs AFTER the turn's terminal event, in the same goroutine — so waiting
	// for the terminal event is waiting for the wrong thing. (Measured: the queue was still full
	// and the log still three events long at that moment.) Wait for the disposition itself.
	deadline := time.Now().Add(10 * time.Second)
	for a.hasPendingInterject(sid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if !answeredIDs(t, a, sid)["m_1"] {
		t.Error("the drain answered it inline and said nothing on the record — " +
			"the bubble stays hoisted to the tail, waiting, under its own answer")
	}
}

// The panel over the terminal says what is waiting on this turn, and opens itself when something
// is. A message the agent has claimed it is answering is not that.
//
// The entry stays queued on purpose — the claim is checked at the finish boundary, because a claim
// is not an answer — but the two readers of that queue had drifted apart: the note the agent sees
// went quiet the moment the claim landed, while this one kept counting. So the panel said
// "Waiting 1", and stayed open for it, through the whole of the reply that answered it.
func TestAClaimedMessageStopsCountingAsWaiting(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.enqueueInterject(ctx, sid, "m_1", "also rename the token")

	if !a.PersonWaiting() || len(a.ParkedWork()) != 1 {
		t.Fatal("a queued message is waiting, and both readers must say so")
	}

	a.markInterjectAnswered(sid, "m_1", a.lastSeq(ctx, sid))

	if got := a.ParkedWork(); len(got) != 0 {
		t.Errorf("the agent is answering it; it is not waiting on anybody: %+v", got)
	}
	if a.PersonWaiting() {
		t.Error("the badge that means 'somebody typed and nobody has got to it' must go out too")
	}
	// It has NOT left the queue — that is the boundary's business, and the claim is still checked.
	if !a.hasPendingInterject(sid) {
		t.Error("a claim is not an answer: the entry must survive for the boundary to test it")
	}
}

// And when the claim does not hold up, it is waiting again — the readers must follow it back.
func TestARevokedClaimCountsAsWaitingAgain(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.enqueueInterject(ctx, sid, "m_1", "also rename the token")
	a.markInterjectAnswered(sid, "m_1", a.lastSeq(ctx, sid))
	if len(a.ParkedWork()) != 0 {
		t.Fatal("claimed, so not waiting")
	}

	// The turn said nothing after the claim, so the boundary revokes it.
	if kept := a.settleAnsweredClaims(ctx, sid); len(kept) != 1 {
		t.Fatalf("an empty claim loses its claim and stays queued, got %+v", kept)
	}
	if got := a.ParkedWork(); len(got) != 1 {
		t.Errorf("the claim was revoked — it is waiting again and must be counted: %+v", got)
	}
	if !a.PersonWaiting() {
		t.Error("the badge must come back with it")
	}
}
