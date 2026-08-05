package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A message typed mid-turn is queued, and the only ways out were a route (redirect/append), the
// finish-boundary drain, or abandonment. An agent that simply ANSWERED it in its reply had no way
// to say so — route_interjection's vocabulary was queue|redirect|append, and "queue" explicitly
// means "it will run as its own turn after the current task". So the request stayed pending after
// being answered, its note re-appeared every step, and the boundary handled it a second time.
// (The one path that did drop it on a reply was handleAside's `case replied`, which lost its
// production caller when the idle-park was removed and has since been deleted with it.)
func TestAnAnsweredClaimStopsTheNoteWithoutLeavingTheQueue(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.enqueueInterject(ctx, sid, "m_1", "also check the header")
	a.noteInterjection(sid, "count the rows", "m_1", "also check the header")

	if note := a.takeInterjectNotes(sid); note == "" {
		t.Fatal("a queued interjection must be shown to the agent")
	}
	a.noteInterjection(sid, "count the rows", "m_1", "also check the header") // re-arm for the next step
	a.markInterjectAnswered(sid, "m_1", 10)

	if note := a.takeInterjectNotes(sid); note != "" {
		t.Errorf("a claimed interjection must stop being advertised as pending:\n%s", note)
	}
	// …but it is NOT gone: the claim is checked at the boundary, so the entry must still be there.
	if !a.hasPendingInterject(sid) {
		t.Error("a claim is not an answer — the entry must survive until the boundary checks it")
	}
}

// The boundary check is the one fact magi can measure: did the turn say anything after the claim?
func TestTheBoundaryKeepsAClaimBackedBySpeechAndRevokesAnEmptyOne(t *testing.T) {
	for _, c := range []struct {
		what      string
		saidAfter bool
		wantQueue int
	}{
		{"the turn spoke after claiming", true, 0},
		{"the turn said nothing after claiming", false, 1},
	} {
		a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
		ctx := context.Background()
		sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
		a.enqueueInterject(ctx, sid, "m_1", "also check the header")

		seq := a.lastSeq(ctx, sid)
		a.markInterjectAnswered(sid, "m_1", seq)
		if c.saidAfter {
			d, _ := json.Marshal(event.PartAppendedData{
				MessageID: "m_a", Role: session.RoleAssistant,
				Part: session.Part{Kind: session.PartText, Text: "the header is on line 1."},
			})
			_ = a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{Kind: event.ActorAgent, ID: "default"}, d)
		}
		got := a.settleAnsweredClaims(ctx, sid)
		if len(got) != c.wantQueue {
			t.Errorf("%s: queue has %d, want %d", c.what, len(got), c.wantQueue)
		}
		// A revoked claim must come back as an ORDINARY queued item, not one that will be
		// re-settled on the next pass — otherwise a silent turn drops it on the second look.
		if len(got) == 1 && got[0].AnsweredAtSeq != 0 {
			t.Errorf("%s: a revoked claim must be cleared, got seq %d", c.what, got[0].AnsweredAtSeq)
		}
	}
}

// Reasoning and tool calls are not replies: a turn that only thought, or only ran commands, after
// claiming to have answered has told the user nothing.
func TestOnlyAssistantTextCountsAsHavingSpoken(t *testing.T) {
	mk := func(kind session.PartKind, role session.Role, text string, seq int64) event.Event {
		d, _ := json.Marshal(event.PartAppendedData{MessageID: "m", Role: role, Part: session.Part{Kind: kind, Text: text}})
		return event.Event{Seq: seq, Type: event.TypePartAppended, Data: d}
	}
	evs := []event.Event{
		mk(session.PartText, session.RoleAssistant, "before the claim", 5),
		mk(session.PartReasoning, session.RoleAssistant, "hmm, the header…", 20),
		mk(session.PartText, session.RoleAssistant, "   ", 21), // whitespace is not speech
	}
	if saidSomethingAfter(evs, 10) {
		t.Error("reasoning and blank text are not a reply")
	}
	if !saidSomethingAfter(append(evs, mk(session.PartText, session.RoleAssistant, "line 1.", 22)), 10) {
		t.Error("assistant text after the claim is speech")
	}
	if saidSomethingAfter(evs, 100) {
		t.Error("nothing follows a claim made after the last event")
	}
}
