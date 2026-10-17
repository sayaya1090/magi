package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The composer's ↑/↓ recall reaches across conversations: a line typed in yesterday's session is
// still there after a switch (2026-10-17 — the terminal's recall died with the session on screen,
// the IDE windows had none). Only what a person typed, newest first, each text once.
func TestRecentPromptsCrossSessionsAndKeepsOnlyTyped(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	wd := t.TempDir()
	say := func(sid session.SessionID, kind event.ActorKind, parts ...session.Part) {
		t.Helper()
		b, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m", Parts: parts})
		if _, err := a.store.Append(ctx, sid, event.Event{Type: event.TypePromptSubmitted, Data: b,
			Actor: event.Actor{Kind: kind}}); err != nil {
			t.Fatal(err)
		}
	}
	text := func(s string) session.Part { return session.Part{Kind: session.PartText, Text: s} }

	old, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	say(old, event.ActorUser, text("fix the parser"))
	say(old, event.ActorUser, text("run the tests"))
	time.Sleep(20 * time.Millisecond) // the newer conversation must be newer by its log's clock

	cur, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	say(cur, event.ActorUser, text("explain this"), text(attachedHeader+"src/a.go:1-3\nfunc a(){}"))
	say(cur, event.ActorSystem, text("You stopped without saying you are finished"))
	say(cur, event.ActorAgent, text("report from the child"))
	say(cur, event.ActorUser, text("run the tests")) // typed again: once, at its newest place

	got, err := a.RecentPrompts(ctx, wd, 10)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, p := range got {
		texts = append(texts, p.Text)
	}
	want := []string{"run the tests", "explain this", "fix the parser"}
	if !reflect.DeepEqual(texts, want) {
		t.Fatalf("recall = %q, want %q", texts, want)
	}
	if got[2].Session != old || got[0].Session != cur {
		t.Fatalf("each prompt names the conversation it came from: %+v", got)
	}

	if two, _ := a.RecentPrompts(ctx, wd, 2); len(two) != 2 {
		t.Fatalf("limit is honoured: %d rows", len(two))
	}
}
