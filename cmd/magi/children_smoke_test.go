package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The real wiring for the `children` door, not a fake.
//
// This tree has already paid for a door that was green against a fake engine and refused every
// time against the real one (session-new: born-lazy creation against a disk membership check).
// The shape of that failure is not reachable from a fake, so every door gets one of these: a real
// store, a real App, the daemonEngine itself.
//
// What it walks: a child records its parent when it is created, the store's scan finds it by that
// parent, and the engine hands it out unreshaped. A child is minted through CreateSession rather
// than spawnChild because that is the same record spawnChild writes — Parent is the field the
// store keys on — and spawnChild is unexported here.
func TestChildrenOfFindsSpawnedChildrenOnTheRealEngine(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	ctx := context.Background()
	wd := t.TempDir()
	user := event.Actor{Kind: event.ActorUser, ID: "cli"}

	parent, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Actor: user})
	if err != nil {
		t.Fatal(err)
	}
	d := daemonEngine{App: a, workdir: wd, handover: handover{at: newWhere(parent)}}

	// No children yet, and that is an ANSWER: the door must not confuse "none" with "cannot say".
	kids, err := d.ChildrenOf(ctx, string(parent))
	if err != nil {
		t.Fatalf("a parent with no children is not an error: %v", err)
	}
	if len(kids) != 0 {
		t.Fatalf("a fresh conversation has no subagents, got %d", len(kids))
	}

	// A child of this parent, and a top-level session beside it. The second one is the control:
	// a listing that returned every session in the workspace would pass the first assertion and
	// still be wrong, which is exactly how a "works on my machine" door ships.
	child, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: wd, Parent: string(parent), Agent: "meeting", Actor: user})
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Actor: user})
	if err != nil {
		t.Fatal(err)
	}
	// Born-lazy: a session is an id until something is written, and the scan that finds children
	// reads FILES. So the child is materialised the way a real spawn materialises one — the
	// created event carries the parent, and that field is what the store keys on.
	//
	// Written through the store rather than Submit: Submit runs a turn, and this test is about
	// the listing, not about a model.
	born := func(sid session.SessionID, parent, agent string) {
		t.Helper()
		d, err := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: agent, Parent: parent})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Append(ctx, sid, event.Event{
			SessionID: sid, Type: event.TypeSessionCreated, Actor: user, TS: time.Now(), Data: d,
		}); err != nil {
			t.Fatal(err)
		}
	}
	born(parent, "", "")
	born(child, string(parent), "meeting")
	born(sibling, "", "")

	kids, err = d.ChildrenOf(ctx, string(parent))
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if len(kids) != 1 {
		t.Fatalf("one child was spawned, the door found %d — a sibling top-level session is not a child", len(kids))
	}
	if kids[0].ID != child {
		t.Fatalf("wrong child: %q, want %q", kids[0].ID, child)
	}
	// The role travels. Without it a screen can list a meeting room and a delegate and say
	// nothing about which is which — the one fact that distinguishes them.
	if kids[0].Agent != "meeting" {
		t.Fatalf("the subagent role is what tells a meeting room from a delegate, got %q", kids[0].Agent)
	}
	// And the child is NOT in the top-level listing — the two doors answer different questions.
	metas, err := d.SessionsHere(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range metas {
		if m.ID == child {
			t.Fatal("a subagent must not appear in the conversation picker — that is why ChildSessions exists")
		}
	}
}
