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

func TestRecentPromptsOrdersByInputTimeRatherThanConversationActivity(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	wd := t.TempDir()
	first, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	appendAt := func(sid session.SessionID, actor event.ActorKind, text string, at time.Time) {
		t.Helper()
		b, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m", Parts: []session.Part{{Kind: session.PartText, Text: text}}})
		if _, err := a.store.Append(ctx, sid, event.Event{Type: event.TypePromptSubmitted, Data: b, TS: at, Actor: event.Actor{Kind: actor}}); err != nil {
			t.Fatal(err)
		}
	}
	appendAt(first, event.ActorUser, "shared", now)
	appendAt(second, event.ActorUser, "shared", now.Add(time.Second))
	appendAt(first, event.ActorUser, "latest typed", now.Add(2*time.Second))
	appendAt(second, event.ActorSystem, "later background activity", now.Add(3*time.Second))
	got, err := a.RecentPrompts(ctx, wd, 1)
	if err != nil || len(got) != 1 || got[0].Text != "latest typed" {
		t.Fatalf("top input must not come from the session with the latest background activity: %+v %v", got, err)
	}
	appendAt(first, event.ActorSystem, "even later background activity", now.Add(4*time.Second))
	got, err = a.RecentPrompts(ctx, wd, 2)
	if err != nil || len(got) != 2 || got[0].Text != "latest typed" || got[1].Text != "shared" || got[1].Session != second {
		t.Fatalf("deduplicate after sorting actual prompt times: %+v %v", got, err)
	}
}
