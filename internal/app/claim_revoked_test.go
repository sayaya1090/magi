package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// deferralsFor returns, in order, what the log says about one message's waiting: true for queued,
// false for resolved.
func deferralsFor(t *testing.T, a *App, sid session.SessionID, msgID string) []bool {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []bool
	for _, e := range evs {
		if e.Type != event.TypeInterjectionDeferred {
			continue
		}
		var d event.InterjectionDeferredData
		if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID {
			out = append(out, !d.Resolved)
		}
	}
	return out
}

// Taking a claim back has to be as loud as making it.
//
// "I have answered that" emits interjection.answered, and every screen reads it as the end of the
// message's wait — the terminal drops the waiting glyph and unpins the bubble from the tail. When
// the boundary then measures that nothing was actually said and puts the entry back in the queue,
// that used to happen entirely inside the process. So the message was waiting on somebody again
// and the only thing the person had been told about it was that it was done: the exact outcome the
// queue exists to prevent, produced by the mechanism built to prevent it.
func TestARevokedClaimIsPutBackOnTheRecord(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.enqueueInterject(ctx, sid, "m_1", "also rename the token")

	// Claimed, and nothing said afterwards.
	a.markInterjectAnswered(sid, "m_1", a.lastSeq(ctx, sid))
	if kept := a.settleAnsweredClaims(ctx, sid); len(kept) != 1 {
		t.Fatalf("an empty claim is revoked and the entry stays queued, got %+v", kept)
	}

	got := deferralsFor(t, a, sid, "m_1")
	if len(got) < 2 {
		t.Fatalf("the log must carry the queueing AND the reversal, got %v", got)
	}
	if !got[len(got)-1] {
		t.Error("the last word about this message is that it is waiting again; the log says otherwise")
	}
}

// A claim that HOLDS is not taken back — the entry really did leave.
func TestAClaimThatHoldsIsNotPutBack(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.enqueueInterject(ctx, sid, "m_1", "also rename the token")
	a.markInterjectAnswered(sid, "m_1", a.lastSeq(ctx, sid))

	// The turn spoke after the claim, which is the one fact magi can measure.
	d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: "there are four files"}})
	if err := a.appendFact(ctx, sid, event.TypePartAppended,
		event.Actor{Kind: event.ActorAgent, ID: "main"}, d); err != nil {
		t.Fatal(err)
	}

	if kept := a.settleAnsweredClaims(ctx, sid); len(kept) != 0 {
		t.Fatalf("a backed claim settles and the entry leaves, got %+v", kept)
	}
	got := deferralsFor(t, a, sid, "m_1")
	if len(got) == 0 || got[len(got)-1] {
		t.Errorf("a settled claim's last word is resolved, got %v", got)
	}
}

// And a reload has to read the reversal the same way.
//
// abandonedDeferrals used to collect "queued" and "resolved" into two sets and subtract, which
// cannot express a message that was queued, claimed, and queued again: the claim sat in the other
// set and won regardless of when it happened. A process restarted in that window would mask a
// request still waiting on somebody, which is how a queued interjection is silently dropped.
func TestAReloadReadsTheReversalAsTheLastWord(t *testing.T) {
	mark := func(id string, resolved bool) event.Event {
		d, _ := json.Marshal(event.InterjectionDeferredData{MessageID: id, Resolved: resolved})
		return event.Event{Type: event.TypeInterjectionDeferred, Data: d}
	}
	answered := func(id string) event.Event {
		d, _ := json.Marshal(event.InterjectionAnsweredData{MessageID: id})
		return event.Event{Type: event.TypeInterjectionAnswered, Data: d}
	}

	// queued → claimed answered → queued again: still waiting.
	got := abandonedDeferrals([]event.Event{mark("m_1", false), answered("m_1"), mark("m_1", false)})
	if !got["m_1"] {
		t.Error("the reversal came last and it says the message is waiting")
	}
	// queued → claimed answered: done, and a reload must not resurrect it.
	got = abandonedDeferrals([]event.Event{mark("m_1", false), answered("m_1")})
	if got["m_1"] {
		t.Error("nothing took that claim back")
	}
	// queued → resolved: done.
	got = abandonedDeferrals([]event.Event{mark("m_1", false), mark("m_1", true)})
	if got["m_1"] {
		t.Error("a resolved deferral is resolved")
	}
}
