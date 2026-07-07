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

// storeApp builds an App over a real jsonl store, torn down cleanly. Shared by the
// bubble-up tests and the (uncommitted) boundary probe.
func storeApp(t *testing.T) (*App, *jsonl.Store) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, completingLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		cc, cx := context.WithTimeout(context.Background(), 5*time.Second)
		defer cx()
		_ = a.Close(cc)
	})
	return a, store
}

// seedSession writes the mandatory session.created so subsequent appends are accepted.
func seedSession(t *testing.T, a *App, sid session.SessionID) {
	t.Helper()
	scd, _ := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir(), Agent: "default"})
	if err := a.appendFact(context.Background(), sid, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorUser, ID: "cli"}, scd); err != nil {
		t.Fatal(err)
	}
}

// A finished child's open concern must cross the boundary onto the parent: re-keyed and
// scoped by the child agent, carrying provenance in Detail, idempotent on re-injection,
// and a no-op when the child raised nothing.
func TestBubbleSubagentConcerns(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	parent := session.SessionID("s_parent")
	child := session.SessionID("s_child")
	seedSession(t, a, parent)
	seedSession(t, a, child)

	// The child (a leaf, no council) raised an unverified-premise concern before finishing.
	if err := a.appendConcernRaised(ctx, child,
		event.Actor{Kind: event.ActorSystem, ID: "council"},
		"self-check/unverified-premise", "self-check", "unverified-premise", "fail",
		"BsaI site never verified", ""); err != nil {
		t.Fatal(err)
	}

	a.bubbleSubagentConcerns(ctx, parent, "explorer", child)

	pevs, _ := a.store.Read(ctx, parent, 0)
	got := sessionConcerns(pevs)
	wantKey := "subagent:explorer/self-check/unverified-premise"
	if !hasKey(got, wantKey) {
		t.Fatalf("parent should carry the re-keyed child concern; got %v", keysOf(got))
	}
	var c Concern
	for _, x := range got {
		if x.Key == wantKey {
			c = x
		}
	}
	if c.Scope != "subagent:explorer" {
		t.Errorf("scope should attribute the child agent, got %q", c.Scope)
	}
	if c.Kind != "unverified-premise" {
		t.Errorf("child kind should be preserved for the council, got %q", c.Kind)
	}
	if want := "[via subagent explorer]"; !contains(c.Detail, want) {
		t.Errorf("detail should carry provenance %q, got %q", want, c.Detail)
	}

	// Idempotent: a second bubble (e.g. a re-injection) must not duplicate the concern.
	a.bubbleSubagentConcerns(ctx, parent, "explorer", child)
	pevs, _ = a.store.Read(ctx, parent, 0)
	n := 0
	for _, x := range sessionConcerns(pevs) {
		if x.Key == wantKey {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("re-injection must not duplicate the concern, found %d", n)
	}
}

// A child that raised nothing (or an empty child sid) bubbles nothing — the parent's
// ledger stays clean, so the boundary carries structural signal only when there is one.
func TestBubbleSubagentConcernsNoop(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	parent := session.SessionID("s_parent")
	child := session.SessionID("s_child")
	seedSession(t, a, parent)
	seedSession(t, a, child)

	a.bubbleSubagentConcerns(ctx, parent, "explorer", child) // child has no concerns
	a.bubbleSubagentConcerns(ctx, parent, "explorer", "")    // empty child sid

	pevs, _ := a.store.Read(ctx, parent, 0)
	if got := sessionConcerns(pevs); len(got) != 0 {
		t.Fatalf("no child concern should bubble nothing, got %v", keysOf(got))
	}
}
