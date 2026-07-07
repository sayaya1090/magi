package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// lookupRecovered gates the auto-resolve of an unverified-premise concern: it must be
// true ONLY when a knowledge lookup actually succeeded this turn, so that mere absence
// of a lookup can never resolve (and thereby launder away) a still-open concern.
func TestLookupRecovered(t *testing.T) {
	fail := func(id string) []event.Event {
		return []event.Event{evToolCall(id, "websearch"), evToolResult(id, "x509", true)}
	}
	ok := func(id, tool string) []event.Event {
		return []event.Event{evToolCall(id, tool), evToolResult(id, "result", false)}
	}
	concat := func(gs ...[]event.Event) []event.Event {
		var out []event.Event
		for _, g := range gs {
			out = append(out, g...)
		}
		return out
	}
	cases := []struct {
		name string
		evs  []event.Event
		want bool
	}{
		{"no lookup at all → not recovered", concat([]event.Event{evPrompt()}, ok("b1", "bash")), false},
		{"failed lookup only → not recovered", concat([]event.Event{evPrompt()}, fail("w1")), false},
		{"a lookup succeeded → recovered", concat([]event.Event{evPrompt()}, fail("w1"), ok("w2", "websearch")), true},
		{"webfetch success counts", concat([]event.Event{evPrompt()}, ok("f1", "webfetch")), true},
		{"success was a PRIOR turn; new prompt resets → not recovered",
			concat([]event.Event{evPrompt()}, ok("w1", "websearch"), []event.Event{evPrompt()}, ok("b1", "bash")), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lookupRecovered(tc.evs); got != tc.want {
				t.Fatalf("lookupRecovered = %v, want %v", got, tc.want)
			}
		})
	}
}

// The append helpers must persist to the real store and fold back correctly: a raise is
// open, a duplicate raise stays one, a resolve closes it, and a re-raise after a resolve
// REOPENS it (the self-healing property, now proven through the on-disk log rather than
// an in-memory slice).
func TestConcernPersistenceRoundTrip(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, completingLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		cc, cx := context.WithTimeout(context.Background(), 5*time.Second)
		defer cx()
		_ = a.Close(cc)
	}()

	sid := session.SessionID("s_concern")
	actor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	key := concernPremiseKey

	// The store requires the first event of a session to be session.created.
	scd, _ := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir(), Agent: "default"})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated, event.Actor{Kind: event.ActorUser, ID: "cli"}, scd); err != nil {
		t.Fatal(err)
	}

	read := func() []Concern {
		evs, _ := store.Read(ctx, sid, 0)
		return sessionConcerns(evs)
	}
	open := func() bool { return hasKey(read(), key) }

	if err := a.appendConcernRaised(ctx, sid, actor, key, "self-check", "unverified-premise", "fail", "d", ""); err != nil {
		t.Fatal(err)
	}
	if !open() {
		t.Fatal("after raise, concern should be open")
	}
	// Duplicate raise → still exactly one open concern (fold dedups by Key).
	_ = a.appendConcernRaised(ctx, sid, actor, key, "self-check", "unverified-premise", "fail", "d2", "")
	if got := read(); len(got) != 1 {
		t.Fatalf("duplicate raise should stay one open concern, got %d", len(got))
	}
	if err := a.appendConcernResolved(ctx, sid, actor, key, "auto", "recovered"); err != nil {
		t.Fatal(err)
	}
	if open() {
		t.Fatal("after resolve, concern should be closed")
	}
	// Re-raise after resolve → reopened (self-healing through the durable log).
	_ = a.appendConcernRaised(ctx, sid, actor, key, "self-check", "unverified-premise", "fail", "again", "")
	if !open() {
		t.Fatal("re-raise after resolve should REOPEN the concern")
	}
}
