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

// SetModel is the single choke point for a runtime model change; it must broadcast
// a model.changed event so any observer (TUI header, /route editor) re-reads the
// model from one signal instead of caching a stale copy.
func TestSetModelBroadcastsEvent(t *testing.T) {
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

	a.SetModel(sid, "new-model")

	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type != event.TypeModelChanged {
				continue
			}
			var d event.ModelChangedData
			if json.Unmarshal(e.Data, &d) != nil || d.Model != "new-model" {
				t.Fatalf("model.changed payload = %q, want new-model", d.Model)
			}
			// Session model updated too (the source of truth).
			if got := a.SessionModel(sid); got != "new-model" {
				t.Fatalf("SessionModel = %q after SetModel, want new-model", got)
			}
			return
		case <-deadline:
			t.Fatal("no model.changed event after SetModel")
		}
	}
}

// AgentRoutes must inherit the SESSION's live model for unrouted agents, so a
// runtime SetModel is reflected — not the static config default (the old split
// source of truth that never updated).
func TestAgentRoutesInheritsSessionModel(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{
		Model:  session.ModelRef{Provider: "openai", Model: "base-model"},
		Agents: map[string]AgentSpec{"coder": {Name: "coder"}}, // unrouted (no Model)
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	if m := routesByName(a.AgentRoutes(sid), "coder").Model; m != "base-model" {
		t.Fatalf("unrouted coder before change = %q, want base-model", m)
	}
	a.SetModel(sid, "new-model")
	if m := routesByName(a.AgentRoutes(sid), "coder").Model; m != "new-model" {
		t.Fatalf("unrouted coder after SetModel = %q, want new-model (session SSOT)", m)
	}
	// An unknown session still falls back to the config default (no session model).
	if m := routesByName(a.AgentRoutes(session.SessionID("nope")), "coder").Model; m != "base-model" {
		t.Fatalf("unknown session coder = %q, want base-model fallback", m)
	}
}
