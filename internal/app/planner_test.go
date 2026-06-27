package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestParsePlan(t *testing.T) {
	// Clean object.
	if p := parsePlan(`{"parallel":true,"groups":[{"agent":"explore","focus":"a","question":"q"}]}`); !p.Parallel || len(p.Groups) != 1 {
		t.Errorf("clean parse failed: %+v", p)
	}
	// Wrapped in prose / fences.
	if p := parsePlan("Sure!\n```json\n{\"parallel\": false}\n```\n"); p.Parallel {
		t.Errorf("prose-wrapped parse: %+v", p)
	}
	// Garbage → zero value (solo).
	if p := parsePlan("no json here"); p.Parallel || p.Groups != nil {
		t.Errorf("garbage should yield zero: %+v", p)
	}
}

func TestSanitizePlan(t *testing.T) {
	// parallel=false → nil regardless of groups.
	if g := sanitizePlan(planResult{Parallel: false, Groups: []planGroup{{Agent: "explore", Question: "q"}}}); g != nil {
		t.Errorf("non-parallel should sanitize to nil, got %v", g)
	}
	// non-explorer coerced to explore; empty question dropped.
	got := sanitizePlan(planResult{Parallel: true, Groups: []planGroup{
		{Agent: "coder", Focus: "x", Question: "find X"}, // coder → explore
		{Agent: "locator", Focus: "y", Question: ""},      // dropped (no question)
		{Agent: "analyst", Focus: "z", Question: "why Z"},
	}})
	if len(got) != 2 {
		t.Fatalf("want 2 groups after sanitize, got %d: %+v", len(got), got)
	}
	if got[0].Agent != "explore" {
		t.Errorf("coder should be coerced to explore, got %q", got[0].Agent)
	}
	// Cap at maxPlanGroups.
	many := planResult{Parallel: true}
	for i := 0; i < maxPlanGroups+3; i++ {
		many.Groups = append(many.Groups, planGroup{Agent: "explore", Question: "q"})
	}
	if g := sanitizePlan(many); len(g) != maxPlanGroups {
		t.Errorf("fan-out should cap at %d, got %d", maxPlanGroups, len(g))
	}
}

// newPlannerApp builds an App with a real store for gating tests.
func newPlannerApp(t *testing.T, cfg Config) (*App, session.SessionID) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, planNoLLM{}, nil, bus.New(), nil, cfg)
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Submit a user prompt event so lastUserPrompt has something.
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: "do A and B"}},
	})
	_ = a.appendFact(context.Background(), sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorUser, ID: "u"}, pd)
	return a, sid
}

// Planner disabled → pre-flight is a no-op (no new events appended).
func TestPlannerDisabledNoOp(t *testing.T) {
	a, sid := newPlannerApp(t, Config{Planner: false})
	before := countEvents(t, a, sid)
	a.maybePlanPreflight(context.Background(), a.sessionInfo(context.Background(), sid))
	if got := countEvents(t, a, sid); got != before {
		t.Errorf("disabled planner should append nothing, events %d→%d", before, got)
	}
}

// Planner enabled but no "planner" agent configured → no-op.
func TestPlannerNoAgentNoOp(t *testing.T) {
	a, sid := newPlannerApp(t, Config{Planner: true}) // no Agents["planner"]
	before := countEvents(t, a, sid)
	a.maybePlanPreflight(context.Background(), a.sessionInfo(context.Background(), sid))
	if got := countEvents(t, a, sid); got != before {
		t.Errorf("missing planner agent should append nothing, events %d→%d", before, got)
	}
}

func countEvents(t *testing.T, a *App, sid session.SessionID) int {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

type planNoLLM struct{}

func (planNoLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	close(ch)
	return ch, nil
}
