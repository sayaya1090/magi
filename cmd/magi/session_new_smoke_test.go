package main

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The real wiring, not a fake: a review proved session-new could never succeed against the actual
// engine — CreateSession is born-lazy (nothing on disk until the first words) while resume's
// membership check reads the disk, so the freshly minted id was refused every time, and the only
// tests were against a fake whose NewSession returned a canned id. This walks daemonEngine itself.
func TestSessionNewSucceedsOnTheRealEngine(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	ctx := context.Background()
	wd := t.TempDir()
	s0, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd,
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}})
	if err != nil {
		t.Fatal(err)
	}
	d := daemonEngine{App: a, workdir: wd, handover: handover{at: newWhere(s0)}}

	// Before anything is spoken, the picker must still show the one conversation the person is in.
	metas, err := d.SessionsHere(ctx)
	if err != nil || len(metas) != 1 || metas[0].ID != s0 {
		t.Fatalf("the unborn current conversation joins the listing: (%+v, %v)", metas, err)
	}

	s1, err := d.NewSession(ctx)
	if err != nil {
		t.Fatalf("session-new refused on the real engine — the reviewed defect: %v", err)
	}
	if s1 == "" || s1 == s0 {
		t.Fatalf("a fresh conversation has a fresh id, got %q (was %q)", s1, s0)
	}
	if got := d.handover.at.now(); got != s1 {
		t.Fatalf("the move is half the verb: current is %q, want %q", got, s1)
	}
	// The old conversation now carries the reason its transcript stops.
	evs, err := st.Read(ctx, s0, 0)
	if err != nil {
		t.Fatal(err)
	}
	moved := false
	for _, e := range evs {
		if e.Type == event.TypeSessionMoved {
			moved = true
		}
	}
	if !moved {
		t.Fatal("the mark goes into the OLD conversation before anything else reads the move")
	}

	// And again, from a conversation that itself never got words — the boot shape.
	s2, err := d.NewSession(ctx)
	if err != nil || s2 == s1 || s2 == "" {
		t.Fatalf("session-new from an unborn current must also work: (%q, %v)", s2, err)
	}
	metas, err = d.SessionsHere(ctx)
	if err != nil || len(metas) < 2 || metas[0].ID != session.SessionID(s2) {
		t.Fatalf("the live conversation tops the picker: (%+v, %v)", metas, err)
	}
}

func TestKeptDocumentSessionIsListedBeforeFirstMessage(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := app.New(store, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	ctx := context.Background()
	t.Cleanup(func() { _ = engine.Close(ctx) })
	wd := t.TempDir()
	current, err := engine.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	d := daemonEngine{App: engine, workdir: wd, handover: handover{at: newWhere(current)}}
	id, err := d.NewSessionKeeping(ctx, "figma:local:document_123")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := d.SessionsHere(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.ID == id {
			count++
			if row.For != "figma:local:document_123" {
				t.Fatal("document association lost")
			}
		}
	}
	if count != 1 {
		t.Fatalf("kept unborn document session count: %d", count)
	}
	if d.handover.at.now() != current {
		t.Fatal("listing or keeping moved the companion")
	}
	persisted, err := store.ListSessions(ctx, wd)
	if err != nil || len(persisted) != 0 {
		t.Fatalf("listing forced empty sessions onto disk: %+v %v", persisted, err)
	}
	other := d
	other.workdir = t.TempDir()
	other.handover.at = newWhere("")
	rows, err = other.SessionsHere(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("foreign workspace leaked: %+v %v", rows, err)
	}
}
