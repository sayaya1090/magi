package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The contract a session tab hangs on: submit targets the SESSION IT NAMES, whatever any record
// says is current. Two sessions each take their own turn — serialization is per session, never
// per companion — and an id nobody minted is refused, not conjured.
func TestSubmitTargetsTheSessionItNames(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	mk := func() session.SessionID {
		sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(),
			Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}})
		if err != nil {
			t.Fatal(err)
		}
		return sid
	}
	s1, s2 := mk(), mk()
	say := func(sid session.SessionID, text string) {
		if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser},
			Parts: []session.Part{{Kind: session.PartText, Text: text}}}); err != nil {
			t.Fatalf("submit to %s: %v", sid, err)
		}
	}
	say(s1, "one")
	say(s2, "two") // while s1 may still be mid-turn: its own turn, its own log

	finished := func(sid session.SessionID) bool {
		evs, _ := a.store.Read(ctx, sid, 0)
		for _, e := range evs {
			if e.Type == event.TypeTurnFinished {
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !(finished(s1) && finished(s2)) {
		time.Sleep(20 * time.Millisecond)
	}
	if !finished(s1) || !finished(s2) {
		t.Fatalf("each named session takes its own turn: s1=%v s2=%v", finished(s1), finished(s2))
	}

	// An id nobody minted: refused by the store's own gate, never conjured into a conversation.
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: "s_invented", Actor: event.Actor{Kind: event.ActorUser},
		Parts: []session.Part{{Kind: session.PartText, Text: "x"}}}); err == nil {
		t.Fatal("an invented session id must be refused")
	}
}
