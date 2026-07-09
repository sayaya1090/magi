package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// steerLLM is a minimal scripted provider (one response per StreamChat call) for the idle-park
// audit tests — standalone so this committed regression does not depend on the local forensic
// probes (probe_aside_handler_test.go), which are excluded from commits.
type steerLLM struct {
	steps [][]port.ProviderEvent
	call  int
}

func (f *steerLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 16)
	var evs []port.ProviderEvent
	if f.call < len(f.steps) {
		evs = f.steps[f.call]
	} else {
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done"}, {Type: port.ProviderFinish}}
	}
	f.call++
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func steerCall(name, args string) []port.ProviderEvent {
	return []port.ProviderEvent{
		{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_" + name, Name: name, Args: json.RawMessage(args)}},
		{Type: port.ProviderFinish},
	}
}

// newSteerApp builds an App with the given scripted provider, the three idle-park signal tools
// registered (production registers them in cmd/magi, not builtin.Default), and an interactive
// top-level session — the surface handleAside operates on.
func newSteerApp(t *testing.T, llm port.LLMProvider) (*App, session.Session) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.RouteInterjection{})
	reg.Register(builtin.CancelDispatch{})
	reg.Register(builtin.AskUser{})
	a := New(store, llm, reg, bus.New(), nil, Config{Permission: "allow", Interactive: true})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return a, a.sessionInfo(context.Background(), sid)
}

// steerAuditText returns the concatenated text of every durable steer-audit record (system
// actor "steer") in a session's store.
func steerAuditText(t *testing.T, a *App, sid session.SessionID) string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorSystem && e.Actor.ID == "steer" {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				b.WriteString(joinPartText(d.Parts))
			}
		}
	}
	return b.String()
}

// A routed steer during idle-park must leave a DURABLE, auditable trace. The raw tool call/result
// stay in the mini-loop (to keep the delegated task's log clean), so without an explicit record
// the transcript shows only the chit-chat reply and never that a redirect/append (or a cancel)
// fired — exactly the audit gap that made the "docs only" steer unverifiable from the log.
func TestAsideRoutePersistsAuditRecord(t *testing.T) {
	llm := &steerLLM{steps: [][]port.ProviderEvent{
		steerCall("route_interjection", `{"action":"append","reason":"only the docs directory"}`),
	}}
	a, s := newSteerApp(t, llm)
	a.enqueueInterject(s.ID, "m1", "only look under docs/")

	if !a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "review the whole repo", "m_b1", "only look under docs/") {
		t.Fatalf("an append steer must act (break the park)")
	}
	rec := steerAuditText(t, a, s.ID)
	for _, want := range []string{"Steer applied", "route_interjection", "append", "only the docs directory", "only look under docs/"} {
		if !strings.Contains(rec, want) {
			t.Errorf("audit record missing %q; got:\n%s", want, rec)
		}
	}
}

// The audit record is masked from interjection detection: it is a system-actor prompt, and every
// interjection path filters to ActorUser, so it must not itself be seen as a new user prompt.
func TestAsideAuditRecordNotSeenAsInterjection(t *testing.T) {
	llm := &steerLLM{steps: [][]port.ProviderEvent{
		steerCall("route_interjection", `{"action":"append","reason":"docs only"}`),
	}}
	a, s := newSteerApp(t, llm)
	a.enqueueInterject(s.ID, "m1", "docs only please")
	a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "task", "m_b2", "docs only please")

	evs, _ := a.store.Read(context.Background(), s.ID, 0)
	for _, e := range userPromptEntries(evs) {
		if strings.Contains(e.Text, "Steer applied") {
			t.Fatalf("steer audit record leaked into user-prompt detection: %q", e.Text)
		}
	}
}

// A pure chitchat aside (no route, no cancel) must NOT emit a steer-audit record — the record is
// reserved for actual steers, so a running log is not polluted with a line per greeting.
func TestAsideChitchatEmitsNoAuditRecord(t *testing.T) {
	llm := &steerLLM{steps: [][]port.ProviderEvent{
		{{Type: port.ProviderText, Text: "still working on it"}, {Type: port.ProviderFinish}},
	}}
	a, s := newSteerApp(t, llm)
	a.enqueueInterject(s.ID, "m1", "how's it going?")
	a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "task", "m_b3", "how's it going?")

	if rec := steerAuditText(t, a, s.ID); rec != "" {
		t.Errorf("chitchat must not emit a steer audit record; got: %q", rec)
	}
}
