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

func TestPlannerWindow(t *testing.T) {
	msg := func(role session.Role, text string) session.Message {
		return session.Message{Role: role, Parts: []session.Part{{Kind: session.PartText, Text: text}}}
	}
	// Short conversation: keep everything, in order, so a follow-up has its context.
	conv := []session.Message{
		msg(session.RoleUser, "C로 헬로월드 작성해서 hell.c로 저장해줘"),
		msg(session.RoleAssistant, "wrote hell.c"),
		msg(session.RoleUser, "개행 두개로 바꿔줘"),
	}
	got := plannerWindow(conv)
	if len(got) != 3 || got[2].Parts[0].Text != "개행 두개로 바꿔줘" {
		t.Fatalf("short conversation should be kept whole, ending at the latest prompt: %d msgs", len(got))
	}
	// Long conversation: bounded to a recent tail, but always includes the last message.
	var long []session.Message
	for i := 0; i < 200; i++ {
		long = append(long, msg(session.RoleUser, strings.Repeat("x", 200)))
	}
	long = append(long, msg(session.RoleUser, "FINAL"))
	w := plannerWindow(long)
	if len(w) >= len(long) {
		t.Errorf("long conversation should be trimmed, got %d of %d", len(w), len(long))
	}
	if w[len(w)-1].Parts[0].Text != "FINAL" {
		t.Errorf("window must end at the latest prompt, got %q", w[len(w)-1].Parts[0].Text)
	}
	// Empty in → empty out (caller falls back to the bare prompt).
	if len(plannerWindow(nil)) != 0 {
		t.Error("nil conversation should yield nil window")
	}
}

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
	// Trailing prose with a stray '}' must NOT break extraction (the old first-{/last-}
	// span over-captured and lost the plan). Balanced scan takes only the object.
	if p := parsePlan(`{"steps":[{"title":"x","strategy":"solo"}]}` + "\n\nLet me know if this works :}"); len(p.Steps) != 1 {
		t.Errorf("trailing-prose-with-brace parse should still yield the plan: %+v", p)
	}
	// A brace inside a string value must not confuse the scan.
	if p := parsePlan(`{"reason":"use {braces} carefully","steps":[{"title":"y","strategy":"solo"}]}`); len(p.Steps) != 1 {
		t.Errorf("brace-in-string parse: %+v", p)
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
		{Title: "d", Strategy: "bogus"},                 // unknown → dropped
		{Title: "e", Strategy: "parallel", Groups: nil}, // no usable groups → dropped
		{Title: "f", Strategy: "scout", Discover: ""},   // no discover → dropped
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

// A scout's discovery agent files its work-list via the report tool, so the reply
// is report-framed ("STATUS: DONE\n<list>"). The frame line must not leak into the
// parsed work-list as a bogus item (it once spawned an "Investigate STATUS: DONE").
func TestParseListStripsReportFrame(t *testing.T) {
	got := parseList(stripReportStatus("STATUS: DONE\n- README.md\n- docs/x.md"))
	want := []string{"README.md", "docs/x.md"}
	if len(got) != len(want) {
		t.Fatalf("parseList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %q, want %q", i, got[i], want[i])
		}
	}
	// BLOCKED frame stripped too.
	if g := parseList(stripReportStatus("STATUS: BLOCKED\nfoo.go\nbar.go")); len(g) != 2 || g[0] != "foo.go" {
		t.Errorf("BLOCKED frame not stripped: %v", g)
	}
	// path:line item is never touched (not a frame line, not first-line-stripped).
	if g := parseList(stripReportStatus("src/foo.go:42\nbar.go")); len(g) != 2 || g[0] != "src/foo.go:42" {
		t.Errorf("path:line item altered: %v", g)
	}
	// Over-strip guard: a multi-word first item starting with "STATUS:" is KEPT.
	if g := parseList(stripReportStatus("STATUS: pending review\nfoo.go")); len(g) == 0 || g[0] != "STATUS: pending review" {
		t.Errorf("multi-word STATUS item wrongly stripped: %v", g)
	}
}

// The planner model sometimes echoes the strategy into the title; renderSteps adds
// it again, so the tag must be stripped from the title to avoid "[scout] [scout]".
func TestStripStrategyTag(t *testing.T) {
	cases := map[string]string{
		"[scout] discover docs":   "discover docs",
		"[solo] define criteria":  "define criteria",
		"[parallel] review (4)":   "review (4)",
		"[SCOUT] caps tag":        "caps tag",
		"no tag here":             "no tag here",
		"[note] not a strategy":   "[note] not a strategy", // unknown bracket left intact
		"[escaped] array literal": "[escaped] array literal",
	}
	for in, want := range cases {
		if got := stripStrategyTag(in); got != want {
			t.Errorf("stripStrategyTag(%q) = %q, want %q", in, got, want)
		}
	}
	// End-to-end: a step whose title echoes its strategy renders without duplication.
	steps := sanitizeSteps(planResult{Steps: []planStep{{Title: "[scout] find docs", Strategy: "scout", Discover: "doc files"}}})
	if len(steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(steps))
	}
	if r := renderSteps(steps); strings.Contains(r, "[scout] [scout]") {
		t.Errorf("strategy tag duplicated: %q", r)
	}
	// A title that is nothing but the tag → emptied, then backfilled to "<strategy> step".
	bare := sanitizeSteps(planResult{Steps: []planStep{{Title: "[scout]", Strategy: "scout", Discover: "x"}}})
	if len(bare) != 1 || bare[0].Title != "scout step" {
		t.Errorf("bare tag title should backfill to %q, got %+v", "scout step", bare)
	}
}

// The planner checks off the steps it actually executes during pre-flight (scout/
// parallel exploration), so the panel reflects real progress even when the model
// never calls todowrite. Solo steps are the main agent's work and stay pending.
func TestExecuteStepsMarksExecutedTodos(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "finding text"}, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 4,
		Agents: map[string]AgentSpec{"explore": {Name: "explore", System: "x"}},
	})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.mu.Unlock()

	steps := []planStep{
		{Title: "define criteria", Strategy: "solo"},
		{Title: "survey files", Strategy: "parallel", Groups: []planGroup{{Agent: "explore", Focus: "f", Question: "q"}}},
		{Title: "write summary", Strategy: "solo"},
	}
	a.registerPlanTodos(context.Background(), s.ID, steps)
	a.executeSteps(context.Background(), s, steps)

	td := a.Todos(s.ID)
	if len(td) != 3 {
		t.Fatalf("want 3 todos, got %d", len(td))
	}
	// The parallel step (index 1) ran → it and the earlier solo step (index 0, which it
	// subsumes) are completed; the trailing solo step stays pending for the main agent.
	if td[0].Status != "completed" || td[1].Status != "completed" {
		t.Errorf("ran step and earlier steps should be completed, got %q / %q", td[0].Status, td[1].Status)
	}
	if td[2].Status != "pending" {
		t.Errorf("trailing solo step should stay pending (main agent handles it), got %q", td[2].Status)
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
// looping forever. The cap is the shared CouncilMaxRounds (default 3), not a
// separate plan-only limit.
func TestPlanAuditCapForcesApprove(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "more"},
		{Round: 2, Decision: council.Continue, Feedback: "more"},
		{Round: 3, Decision: council.Continue, Feedback: "more", Criteria: []string{"build passes"}},
	}}
	replan := textStep(`{"steps":[{"title":"A","strategy":"solo"},{"title":"B","strategy":"solo"}]}`)
	a, wd := newApp(t, &fakeLLM{steps: [][]port.ProviderEvent{replan, replan, replan}},
		Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	got := a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "p", steps)
	if len(got) == 0 {
		t.Fatal("cap should still yield a plan to execute")
	}
	dec := planDecisions(t, a, s.ID)
	// Default cap is 3 rounds (shared with the termination gate), so it runs three
	// revise rounds before force-approving — not the old hardcoded 2.
	if len(dec) != 3 {
		t.Fatalf("want 3 plan-audit rounds at the default cap, got %d: %+v", len(dec), dec)
	}
	last := dec[len(dec)-1]
	if last.Decision != string(council.Done) || !strings.Contains(last.Note, "unresolved after 3") {
		t.Fatalf("cap should force a noted approve after 3 rounds, got %+v", last)
	}
	// Force-approve still keeps the proceeding plan's criteria as the contract.
	if c := a.cachedCriteria(s.ID); !strings.Contains(c, "build passes") {
		t.Fatalf("force-approve should store the plan's criteria, got %q", c)
	}
}

// A revise whose re-plan keeps failing (empty/unparseable, incl. the retry) must
// proceed with the prior plan but say WHY (a noted finish) — not silently run the
// rejected plan with no explanation.
func TestPlanAuditReplanFailProceedsWithNote(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "fix it"},
	}}
	bad := textStep("sorry, I can't produce json") // no parseable steps → empty re-plan
	a, wd := newApp(t, &fakeLLM{steps: [][]port.ProviderEvent{bad, bad}},
		Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	got := a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "p", steps)
	if len(got) != len(steps) {
		t.Fatalf("re-plan failure should keep the prior plan, got %d want %d", len(got), len(steps))
	}
	dec := planDecisions(t, a, s.ID)
	last := dec[len(dec)-1]
	if last.Decision != string(council.Done) || !strings.Contains(last.Note, "re-plan failed") {
		t.Fatalf("re-plan failure should emit a 'proceeding' note, got %+v", last)
	}
}

// The plan-audit cap follows the configured CouncilMaxRounds (here 1), proving it's
// the shared knob and not a hardcoded constant.
func TestPlanAuditCapRespectsCouncilMaxRounds(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "more"},
	}}
	a, wd := newApp(t, &fakeLLM{}, Config{
		Council: fc, CouncilMaxRounds: 1,
		Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}},
	})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "p", steps)
	dec := planDecisions(t, a, s.ID)
	if len(dec) != 1 {
		t.Fatalf("CouncilMaxRounds=1 should cap at a single round, got %d", len(dec))
	}
	if !strings.Contains(dec[0].Note, "unresolved after 1") {
		t.Fatalf("single-round cap should force-approve with a 1-round note, got %+v", dec[0])
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
