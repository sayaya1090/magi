package tui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// ↑ reaches what was typed in ANOTHER conversation of this workspace — on a fresh start and after a
// switch. The list used to be the prompts of the conversation on screen, so a switch emptied it
// (2026-10-17).
func TestRecallReachesOtherConversations(t *testing.T) {
	ctx := context.Background()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(store, stubLLM{}, builtin.Default(), bus.New(), nil, app.Config{Permission: "allow"})
	wd := t.TempDir()
	old, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	// Straight to the store, the way resume_test's fixtures do: a session with content, not a turn.
	born, _ := json.Marshal(event.SessionCreatedData{Workdir: wd})
	b, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m1",
		Parts: []session.Part{{Kind: session.PartText, Text: "yesterday's question"}}})
	if _, err := store.Append(ctx, old,
		event.Event{SessionID: old, Type: event.TypeSessionCreated, TS: time.Now(), Data: born},
		event.Event{SessionID: old, Type: event.TypePromptSubmitted, TS: time.Now(), Data: b,
			Actor: event.Actor{Kind: event.ActorUser, ID: "tui"}}); err != nil {
		t.Fatal(err)
	}
	fresh, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	m := New(ctx, a, nil, fresh, "m", wd, true, "")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.ta.Value() != "yesterday's question" {
		t.Fatalf("↑ in a fresh conversation = %q, want the line typed in the other one", m.ta.Value())
	}

	m.ta.SetValue("")
	m.switchSession(fresh)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.ta.Value() != "yesterday's question" {
		t.Fatalf("↑ after a switch = %q, want the line typed in the other one", m.ta.Value())
	}
}
