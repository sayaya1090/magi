package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// SetUserLabel is the choke point for a plugin-injected transcript user label; it
// stores the label on the session and broadcasts user.label.changed so the TUI
// re-reads it from one signal (mirrors SetModel). A later reader sees the value.
func TestSetUserLabelBroadcastsEvent(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{
		Model: session.ModelRef{Provider: "openai", Model: "base-model"},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()

	a.SetUserLabel(sid, "Alice")

	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type != event.TypeUserLabelChanged {
				continue
			}
			var d event.UserLabelData
			if json.Unmarshal(e.Data, &d) != nil || d.Label != "Alice" {
				t.Fatalf("user.label.changed payload = %q, want Alice", d.Label)
			}
			if got := a.UserLabel(sid); got != "Alice" {
				t.Fatalf("UserLabel = %q after SetUserLabel, want Alice", got)
			}
			return
		case <-deadline:
			t.Fatal("no user.label.changed event after SetUserLabel")
		}
	}
}

// An empty or whitespace-only label is ignored: no state change, no broadcast.
func TestSetUserLabelIgnoresEmpty(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{
		Model: session.ModelRef{Provider: "openai", Model: "base-model"},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	a.SetUserLabel(sid, "   ")
	if got := a.UserLabel(sid); got != "" {
		t.Fatalf("whitespace label should be ignored, got %q", got)
	}
}
