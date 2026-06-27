package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestParsePlan(t *testing.T) {
	// Clean object with an ordered procedure.
	p := parsePlan(`{"reason":"r","steps":[{"title":"t","strategy":"parallel","groups":[{"agent":"explore","focus":"a","question":"q"}]}]}`)
	if len(p.Steps) != 1 || p.Steps[0].Strategy != "parallel" || len(p.Steps[0].Groups) != 1 {
		t.Errorf("clean parse failed: %+v", p)
	}
	// Wrapped in prose / fences.
	if p := parsePlan("Sure!\n```json\n{\"steps\":[{\"title\":\"x\",\"strategy\":\"solo\"}]}\n```\n"); len(p.Steps) != 1 {
		t.Errorf("prose-wrapped parse: %+v", p)
	}
	// Garbage → zero value (solo).
	if p := parsePlan("no json here"); len(p.Steps) != 0 {
		t.Errorf("garbage should yield no steps: %+v", p)
	}
}

func TestSanitizeSteps(t *testing.T) {
	got := sanitizeSteps(planResult{Steps: []planStep{
		{Title: "a", Strategy: "solo"}, // kept (structures the procedure)
		{Title: "b", Strategy: "parallel", Groups: []planGroup{
			{Agent: "coder", Focus: "x", Question: "find X"}, // coder → explore
			{Agent: "locator", Question: ""},                 // dropped (no question)
		}},
		{Title: "c", Strategy: "scout", Agent: "writer", Discover: "docs/*.md", Each: "summarize"}, // writer → explore
		{Title: "d", Strategy: "bogus"},                       // unknown → dropped
		{Title: "e", Strategy: "parallel", Groups: nil},        // no usable groups → dropped
		{Title: "f", Strategy: "scout", Discover: ""},          // no discover → dropped
	}})
	if len(got) != 3 {
		t.Fatalf("want 3 steps (solo, parallel, scout), got %d: %+v", len(got), got)
	}
	if got[1].Strategy != "parallel" || len(got[1].Groups) != 1 || got[1].Groups[0].Agent != "explore" {
		t.Errorf("parallel: non-explorer should be coerced, empty-question dropped: %+v", got[1])
	}
	if got[2].Strategy != "scout" || got[2].Agent != "explore" {
		t.Errorf("scout: non-explorer agent should be coerced to explore: %+v", got[2])
	}

	// Step count caps at maxPlanSteps.
	many := planResult{}
	for i := 0; i < maxPlanSteps+3; i++ {
		many.Steps = append(many.Steps, planStep{Title: "s", Strategy: "solo"})
	}
	if g := sanitizeSteps(many); len(g) != maxPlanSteps {
		t.Errorf("steps should cap at %d, got %d", maxPlanSteps, len(g))
	}
}

// parseList turns a scout explorer's free-text reply into a clean work-list.
func TestParseList(t *testing.T) {
	got := parseList("```\n1. ARCHITECTURE.md\n- DESIGN.md\n* PLAN.md\n\nHere is a sentence that is clearly prose and not a list item at all really\nSPEC.md\n```")
	want := []string{"ARCHITECTURE.md", "DESIGN.md", "PLAN.md", "SPEC.md"}
	if len(got) != len(want) {
		t.Fatalf("parseList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %q, want %q", i, got[i], want[i])
		}
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

// planAuditFixture makes a session and a two-step procedure to audit.
func planAuditFixture(t *testing.T, a *App, wd string) (session.Session, []planStep) {
	t.Helper()
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := []planStep{
		{Title: "A", Strategy: "solo"},
		{Title: "B", Strategy: "parallel", Groups: []planGroup{{Agent: "explore", Focus: "f", Question: "q"}}},
	}
	return a.sessionInfo(context.Background(), sid), steps
}

func planDecisions(t *testing.T, a *App, sid session.SessionID) []event.CouncilDecidedData {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []event.CouncilDecidedData
	for _, e := range evs {
		if e.Type == event.TypeCouncilDecided {
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) == nil && d.Phase == "plan" {
				out = append(out, d)
			}
		}
	}
	return out
}

// An approved plan is returned unchanged, with a phase=plan decided event, and the
// council's derived completion criteria become the turn's termination contract.
func TestPlanAuditApproves(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Done, Criteria: []string{"hello.txt exists", "tests pass"}},
	}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	got := a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps)
	if len(got) != 2 {
		t.Fatalf("approve should keep the plan, got %d steps", len(got))
	}
	dec := planDecisions(t, a, s.ID)
	if len(dec) != 1 || dec[0].Decision != string(council.Done) || dec[0].Note != "" {
		t.Fatalf("want one clean plan-approve decision, got %+v", dec)
	}
	if len(dec[0].Criteria) != 2 {
		t.Fatalf("decided should carry the derived criteria, got %v", dec[0].Criteria)
	}
	// The criteria are stored as the contract the termination gate will read.
	if c := a.cachedCriteria(s.ID); !strings.Contains(c, "hello.txt exists") || !strings.Contains(c, "tests pass") {
		t.Fatalf("plan-derived criteria should be cached as the contract, got %q", c)
	}
}

// A revise verdict re-plans via the planner LLM; the next round approves the
// revised procedure, which is what gets returned.
func TestPlanAuditRevisesThenReplans(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "add a verify step"},
		{Round: 2, Decision: council.Done},
	}}
	replanned := `{"reason":"r","steps":[{"title":"X","strategy":"solo"},{"title":"Y","strategy":"solo"},{"title":"Z verify","strategy":"solo"}]}`
	a, wd := newApp(t, &fakeLLM{steps: [][]port.ProviderEvent{textStep(replanned)}},
		Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	got := a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps)
	if len(got) != 3 || got[2].Title != "Z verify" {
		t.Fatalf("revise should re-plan to the new procedure, got %+v", got)
	}
}

// Persistent revise hits the round cap and force-approves (a noted finish), never
// looping forever.
func TestPlanAuditCapForcesApprove(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "more"},
		{Round: 2, Decision: council.Continue, Feedback: "more", Criteria: []string{"build passes"}},
	}}
	replan := textStep(`{"steps":[{"title":"A","strategy":"solo"},{"title":"B","strategy":"solo"}]}`)
	a, wd := newApp(t, &fakeLLM{steps: [][]port.ProviderEvent{replan, replan}},
		Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	got := a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "p", steps)
	if len(got) == 0 {
		t.Fatal("cap should still yield a plan to execute")
	}
	dec := planDecisions(t, a, s.ID)
	last := dec[len(dec)-1]
	if last.Decision != string(council.Done) || !strings.Contains(last.Note, "unresolved") {
		t.Fatalf("cap should force a noted approve, got %+v", last)
	}
	// Force-approve still keeps the proceeding plan's criteria as the contract.
	if c := a.cachedCriteria(s.ID); !strings.Contains(c, "build passes") {
		t.Fatalf("force-approve should store the plan's criteria, got %q", c)
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
