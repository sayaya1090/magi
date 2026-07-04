package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// recLLM records the full text (system + messages) of every request and replies with
// reply(req) — enabling assertions on which prompts were issued (e.g. did the failure
// retry send a decomposition-framed prompt) and content-driven success/failure.
type recLLM struct {
	mu      sync.Mutex
	prompts []string
	reply   func(req string) string // nil → always empty (every attempt "fails")
}

func (r *recLLM) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	var b strings.Builder
	b.WriteString(req.System)
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			b.WriteString(p.Text)
		}
	}
	s := b.String()
	r.mu.Lock()
	r.prompts = append(r.prompts, s)
	r.mu.Unlock()
	out := ""
	if r.reply != nil {
		out = r.reply(s)
	}
	ch := make(chan port.ProviderEvent, 4)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: out}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (r *recLLM) sawDecomposeRetry() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.prompts {
		if strings.Contains(p, "BREAK IT DOWN") {
			return true
		}
	}
	return false
}

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
	// Multi-word header/preamble/closing lines the model prints around its list are
	// NOT items (one once made an explorer chase "List of documentation files:" until
	// the subagent timeout). A real path/item is a single token, or short and not
	// ending in sentence/heading punctuation.
	got2 := parseList("List of documentation files:\n- README.md\nHere are the docs.\n- docs/x.md\nThat's all!")
	if len(got2) != 2 || got2[0] != "README.md" || got2[1] != "docs/x.md" {
		t.Errorf("header/preamble/closing lines should be dropped, got %v", got2)
	}
	// Single-token items are kept even with a trailing colon (a config key, a label).
	if g := parseList("server:\nfoo.go"); len(g) != 2 || g[0] != "server:" {
		t.Errorf("single-token 'server:' should be kept, got %v", g)
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
	a.executeSteps(context.Background(), s, steps, 0)

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

// A delegate step needs a work instruction; heterogeneous strategies coexist in one
// plan (a single tree carries solo/parallel/scout/delegate branches side by side).
func TestSanitizeStepsDelegate(t *testing.T) {
	got := sanitizeSteps(planResult{Steps: []planStep{
		{Title: "solo it", Strategy: "solo"},
		{Title: "survey", Strategy: "parallel", Groups: []planGroup{{Agent: "explore", Focus: "f", Question: "q"}}},
		{Title: "scout docs", Strategy: "scout", Discover: "docs/*.md", Each: "read"},
		{Title: "build the CLI", Strategy: "delegate", Agent: " coder ", Task: "write cmd/foo"}, // kept; agent trimmed
		{Title: "empty task", Strategy: "delegate", Agent: "coder", Task: "   "},                // dropped (no work)
	}})
	if len(got) != 4 {
		t.Fatalf("want 4 steps (solo/parallel/scout/delegate), got %d: %+v", len(got), got)
	}
	d := got[3]
	if d.Strategy != "delegate" || d.Agent != "coder" || d.Task != "write cmd/foo" {
		t.Errorf("delegate step not preserved/trimmed: %+v", d)
	}
	// The four surviving strategies must all differ — heterogeneity within one plan.
	seen := map[string]bool{}
	for _, st := range got {
		seen[st.Strategy] = true
	}
	for _, want := range []string{"solo", "parallel", "scout", "delegate"} {
		if !seen[want] {
			t.Errorf("strategy %q missing from a heterogeneous plan: %+v", want, got)
		}
	}
}

// planEligible is the single recursion gate: only a producing agent plans, only below
// the depth cap, never in workflow mode.
func TestPlanEligible(t *testing.T) {
	a := &App{cfg: Config{Planner: true, MaxPlanDepth: 2}}
	coder := AgentSpec{Name: "coder", Tools: []string{"read", "write", "edit", "bash"}}
	readonly := AgentSpec{Name: "explore", Tools: []string{"read", "grep", "glob"}}
	// A verifier holds bash to RUN checks but authors no files: it must NOT be plan-eligible,
	// or it would re-plan (and possibly dispatch writers) during the independent review pass.
	tester := AgentSpec{Name: "tester", Tools: []string{"read", "grep", "bash"}}

	if !a.planEligible(coder, 0) || !a.planEligible(coder, 1) {
		t.Error("a producing agent below the cap should be plan-eligible")
	}
	if a.planEligible(coder, 2) {
		t.Error("depth == MaxPlanDepth must stop recursion (bounded tree)")
	}
	if a.planEligible(readonly, 0) {
		t.Error("a read-only explorer is a leaf — it must not re-plan")
	}
	if a.planEligible(tester, 0) {
		t.Error("a run-only verifier (bash, no write/edit) must not re-plan")
	}
	a.cfg.Workflow = true
	if a.planEligible(coder, 0) {
		t.Error("workflow mode owns staging — no pre-flight planning")
	}
	a.cfg.Workflow, a.cfg.Planner = false, false
	if a.planEligible(coder, 0) {
		t.Error("planner disabled → not eligible")
	}
}

// Only configured, execute-capable agents may be a delegate's executor; the planner
// itself and read-only agents are excluded, so a bogus target degrades to solo.
func TestDelegateAgentResolution(t *testing.T) {
	a := &App{cfg: Config{Agents: map[string]AgentSpec{
		"coder":   {Name: "coder", Tools: []string{"read", "write", "edit", "bash"}},
		"explore": {Name: "explore", Tools: []string{"read", "grep"}},
		"tester":  {Name: "tester", Tools: []string{"read", "grep", "bash"}}, // runs checks, authors nothing
		"planner": {Name: "planner", Tools: []string{"read", "write"}},       // execute-capable but reserved
	}}}
	if names := a.delegatableAgents(); len(names) != 1 || names[0] != "coder" {
		t.Errorf("delegatableAgents = %v, want [coder] (explore/tester author nothing, planner is reserved)", names)
	}
	if n, ok := a.delegateAgentName("coder"); !ok || n != "coder" {
		t.Errorf("coder should resolve as a delegate executor, got %q %v", n, ok)
	}
	for _, bad := range []string{"", "explore", "tester", "planner", "ghost"} {
		if _, ok := a.delegateAgentName(bad); ok {
			t.Errorf("%q must NOT resolve as a delegate executor", bad)
		}
	}
}

// A delegate step dispatches its executor (recursive execution), merges the report,
// flags delegated=true, and checks the todo off.
func TestExecuteStepsDelegate(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "built and verified"}, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 4,
		Agents: map[string]AgentSpec{"coder": {Name: "coder", System: "x", Tools: []string{"read", "write", "edit", "bash"}}},
	})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.mu.Unlock()

	steps := []planStep{
		{Title: "prep", Strategy: "solo"},
		{Title: "build X", Strategy: "delegate", Agent: "coder", Task: "write cmd/x"},
	}
	a.registerPlanTodos(context.Background(), s.ID, steps)
	findings, delegated := a.executeSteps(context.Background(), s, steps, 0)

	if !delegated {
		t.Error("a dispatched delegate step must set delegated=true")
	}
	if !strings.Contains(findings, "built and verified") || !strings.Contains(findings, "(delegated to coder)") {
		t.Errorf("delegate report not merged into findings: %q", findings)
	}
	td := a.Todos(s.ID)
	if td[0].Status != "completed" || td[1].Status != "completed" {
		t.Errorf("delegate step and subsumed earlier step should be completed, got %q / %q", td[0].Status, td[1].Status)
	}
}

// A delegate naming a read-only or unknown executor dispatches nothing (degrades to
// solo): the main agent will do that work, so its todo stays pending.
func TestExecuteStepsDelegateInvalidAgentDegrades(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "should not run"}, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 4,
		Agents: map[string]AgentSpec{"explore": {Name: "explore", System: "x", Tools: []string{"read", "grep"}}},
	})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.mu.Unlock()

	steps := []planStep{{Title: "build X", Strategy: "delegate", Agent: "explore", Task: "write cmd/x"}}
	a.registerPlanTodos(context.Background(), s.ID, steps)
	findings, delegated := a.executeSteps(context.Background(), s, steps, 0)

	if delegated || strings.TrimSpace(findings) != "" {
		t.Errorf("invalid delegate executor should dispatch nothing, got delegated=%v findings=%q", delegated, findings)
	}
	if td := a.Todos(s.ID); td[0].Status != "pending" {
		t.Errorf("degraded delegate todo should stay pending for the main agent, got %q", td[0].Status)
	}
}

// A delegate whose sub-agent returns nothing (failed/empty) is UNFINISHED: its todo must
// stay pending and it must NOT flag delegated (which would tell the parent, via the redo-
// prevention directive, to skip re-doing the failed work). The findings note it as FAILED.
func TestExecuteStepsDelegateFailureStaysPending(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: ""}, Config{ // empty sub-agent result = no work done
		Permission: "allow", MaxAgents: 100, MaxDepth: 4,
		Agents: map[string]AgentSpec{"coder": {Name: "coder", System: "x", Tools: []string{"read", "write", "edit", "bash"}}},
	})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.sessions[s.ID] = s
	a.mu.Unlock()

	steps := []planStep{{Title: "build X", Strategy: "delegate", Agent: "coder", Task: "write cmd/x"}}
	a.registerPlanTodos(context.Background(), s.ID, steps)
	findings, delegated := a.executeSteps(context.Background(), s, steps, 0)

	if delegated {
		t.Error("a failed/empty delegate must NOT set delegated=true (would suppress redo)")
	}
	if strings.Contains(findings, "(delegated to") {
		t.Errorf("failed delegate must not be marked '(delegated to …)': %q", findings)
	}
	if !strings.Contains(findings, "FAILED") {
		t.Errorf("failed delegate should be recorded as FAILED so the parent redoes it: %q", findings)
	}
	if td := a.Todos(s.ID); td[0].Status != "pending" {
		t.Errorf("failed delegate todo must stay pending, got %q", td[0].Status)
	}
}

// ADaPT failure branch: a hard-failed delegate below the plan-depth cap is retried ONCE with
// a decomposition-framed prompt (the executor re-plans smaller). If the retry recovers, the
// step completes; if it still fails, the todo stays pending. At the cap, no retry fires.
func TestExecuteStepsDelegateFailureRecursion(t *testing.T) {
	newApp := func(llm port.LLMProvider) (*App, session.Session) {
		a := newOrchApp(t, llm, Config{
			Permission: "allow", MaxAgents: 100, MaxDepth: 5, MaxPlanDepth: 2,
			Agents: map[string]AgentSpec{"coder": {Name: "coder", System: "x", Tools: []string{"read", "write", "edit", "bash"}}},
		})
		s := parentSession(t.TempDir())
		a.mu.Lock()
		a.sessions[s.ID] = s
		a.mu.Unlock()
		a.registerPlanTodos(context.Background(), s.ID, []planStep{{Title: "build X", Strategy: "delegate", Agent: "coder", Task: "write cmd/x"}})
		return a, s
	}
	steps := []planStep{{Title: "build X", Strategy: "delegate", Agent: "coder", Task: "write cmd/x"}}

	// Recovery: first attempt returns empty (fail); the decomposition retry returns work.
	t.Run("retry recovers", func(t *testing.T) {
		llm := &recLLM{reply: func(req string) string {
			if strings.Contains(req, "BREAK IT DOWN") {
				return "decomposed and built it"
			}
			return "" // the direct attempt fails
		}}
		a, s := newApp(llm)
		findings, delegated := a.executeSteps(context.Background(), s, steps, 0)
		if !llm.sawDecomposeRetry() {
			t.Error("a hard failure below the cap must trigger the decomposition-framed retry")
		}
		if !delegated || !strings.Contains(findings, "decomposed and built it") {
			t.Errorf("a recovered retry should complete the step: delegated=%v findings=%q", delegated, findings)
		}
		if td := a.Todos(s.ID); td[0].Status != "completed" {
			t.Errorf("recovered delegate todo should be completed, got %q", td[0].Status)
		}
	})

	// Both attempts fail: the retry still fires, but the todo stays pending for the parent.
	t.Run("retry still fails", func(t *testing.T) {
		llm := &recLLM{} // every attempt returns empty
		a, s := newApp(llm)
		_, delegated := a.executeSteps(context.Background(), s, steps, 0)
		if !llm.sawDecomposeRetry() {
			t.Error("the retry must fire even if it ultimately fails")
		}
		if delegated {
			t.Error("two failed attempts must not mark the step delegated")
		}
		if td := a.Todos(s.ID); td[0].Status != "pending" {
			t.Errorf("an unrecovered delegate todo must stay pending, got %q", td[0].Status)
		}
	})

	// At the recursion cap (depth+1 == MaxPlanDepth), the first attempt still runs but no
	// retry is attempted, and the unrecovered step stays pending for the parent.
	t.Run("no retry at the cap", func(t *testing.T) {
		llm := &recLLM{} // always fails
		a, s := newApp(llm)
		a.executeSteps(context.Background(), s, steps, 1) // depth 1, cap 2 → 2 < 2 is false
		if len(llm.prompts) == 0 {
			t.Fatal("the first delegate attempt must still run at the cap")
		}
		if llm.sawDecomposeRetry() {
			t.Error("at the plan-depth cap the failure retry must be suppressed (bounded recursion)")
		}
		if td := a.Todos(s.ID); td[0].Status != "pending" {
			t.Errorf("an unrecovered delegate at the cap must stay pending, got %q", td[0].Status)
		}
	})
}

// keepScoutItem keeps single-token targets and real in-tree paths, but drops multi-word
// prose (a header/sentence the model emitted around its list) that doesn't exist as a
// path — the leak that sent an explorer chasing a nonexistent target until it timed out.
func TestKeepScoutItem(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		item string
		want bool
	}{
		{"README.md", true},     // single token, exists
		{"docs/x.md", true},     // single token (not validated)
		{"src/foo.go:42", true}, // single token with colon — kept, not Stat'd
		{"parseList", true},     // single-token symbol
		{"List of files in project root and docs directory", false}, // the leaked header — multi-word, no such path
		{"user authentication", false},                              // multi-word non-path (accepted topic tradeoff)
		{"my notes.md", true},                                       // multi-word but a real path → rescued
		{`"my notes.md"`, true},                                     // quote-wrapped real path
		{"../../etc hosts", false},                                  // escapes workdir
		{"", false},
	}
	for _, c := range cases {
		if got := keepScoutItem(dir, c.item); got != c.want {
			t.Errorf("keepScoutItem(%q) = %v, want %v", c.item, got, c.want)
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
	a.maybePlanPreflight(context.Background(), a.sessionInfo(context.Background(), sid), 0)
	if got := countEvents(t, a, sid); got != before {
		t.Errorf("disabled planner should append nothing, events %d→%d", before, got)
	}
}

// Planner enabled but no "planner" agent configured → no-op.
func TestPlannerNoAgentNoOp(t *testing.T) {
	a, sid := newPlannerApp(t, Config{Planner: true}) // no Agents["planner"]
	before := countEvents(t, a, sid)
	a.maybePlanPreflight(context.Background(), a.sessionInfo(context.Background(), sid), 0)
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

// A CRITICAL revise verdict re-plans via the planner LLM; the next round approves
// the revised procedure, which is what gets returned. (Only critical blocks.)
func TestPlanAuditRevisesThenReplans(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue, Severity: council.SeverityCritical, Feedback: "add a verify step"},
		}},
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
	crit := func(fb string) []council.Verdict {
		return []council.Verdict{{Member: "Melchior", Lens: "correctness", Decision: council.Continue, Severity: council.SeverityCritical, Feedback: fb}}
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Verdicts: crit("more")},
		{Round: 2, Decision: council.Continue, Verdicts: crit("more")},
		{Round: 3, Decision: council.Continue, Verdicts: crit("more"), Criteria: []string{"build passes"}},
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
		{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue, Severity: council.SeverityCritical, Feedback: "fix it"},
		}},
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

// A non-blocking (warn/info) revision does NOT re-plan: the plan is approved in one
// round, the advice is injected as a system prompt for the executor to heed, and the
// council's criteria are kept as the termination contract. This is the budget-saving
// path — sub-critical concerns no longer loop the planner.
func TestPlanAuditWarnProceedsWithAdvice(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Criteria: []string{"output.txt exists"},
			Verdicts: []council.Verdict{
				{Member: "Casper", Lens: "completeness", Decision: council.Continue, Severity: council.SeverityWarn, Feedback: "consider adding a test"},
			}},
	}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	got := a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps)

	if len(got) != 2 {
		t.Fatalf("warn-only must not re-plan; want the original 2 steps, got %d", len(got))
	}
	if fc.calls != 1 {
		t.Fatalf("warn-only must not loop the council; calls=%d", fc.calls)
	}
	dec := planDecisions(t, a, s.ID)
	if len(dec) != 1 || dec[0].Decision != string(council.Done) || !strings.Contains(dec[0].Note, "advisory") {
		t.Fatalf("want one approve-with-advisory decision, got %+v", dec)
	}
	if c := a.cachedCriteria(s.ID); !strings.Contains(c, "output.txt exists") {
		t.Fatalf("criteria should be kept as the contract, got %q", c)
	}
	// The advice is injected as a system prompt the executor will see this turn.
	evs, err := a.store.Read(context.Background(), s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawAdvice bool
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		for _, p := range d.Parts {
			if strings.Contains(p.Text, "Plan review") && strings.Contains(p.Text, "consider adding a test") {
				sawAdvice = true
			}
		}
	}
	if !sawAdvice {
		t.Fatal("warn advice should be injected as a system prompt for the executor")
	}
}

// The plan-audit cap follows the configured CouncilMaxRounds (here 1), proving it's
// the shared knob and not a hardcoded constant.
func TestPlanAuditCapRespectsCouncilMaxRounds(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Continue, Severity: council.SeverityCritical, Feedback: "more"},
		}},
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
