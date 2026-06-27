package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// plannerAgent is the agent name the pre-flight planner is configured under (its
// system prompt, model, and provider come from cfg.Agents["planner"], so it is
// routable like any other agent, e.g. [routing] planner = "fast").
const plannerAgent = "planner"

const (
	maxPlanGroups    = 5 // explorers per single parallel/scout fan-out
	maxPlanSteps     = 6 // ordered steps the planner may propose
	maxPlanExplorers = 8 // per-turn TOTAL explorer spawns (a per-turn budget, not the lifetime MaxAgents)
)

// planGroup is one independent investigation to parallelize.
type planGroup struct {
	Agent    string `json:"agent"`    // read-only explorer: explore|locator|analyst
	Focus    string `json:"focus"`    // short label of the area
	Question string `json:"question"` // what this explorer should find out
}

// planStep is one ordered step of the procedure plus HOW to execute it (D17).
type planStep struct {
	Title    string      `json:"title"`            // human-facing step (becomes a todo)
	Strategy string      `json:"strategy"`         // solo | parallel | scout
	Groups   []planGroup `json:"groups,omitempty"` // parallel: explorers to fan out
	// scout (adaptive): discover a work-list at runtime, then fan out one explorer
	// per item — this is what lets "list the docs, then read each in parallel" be
	// expressed without the planner knowing the list in advance.
	Agent    string `json:"agent,omitempty"`    // scout + per-item explorer (read-only)
	Discover string `json:"discover,omitempty"` // what list to produce, e.g. "the markdown files under docs/"
	Each     string `json:"each,omitempty"`     // what to find out about each discovered item
}

// planResult is the planner's procedure: an ordered list of steps.
type planResult struct {
	Steps  []planStep `json:"steps"`
	Reason string     `json:"reason"`
}

// readOnlyExplorers are the only agents the planner may dispatch — investigation
// is read-only, so there are no file conflicts and nothing to fabricate-then-write.
var readOnlyExplorers = map[string]bool{"explore": true, "locator": true, "analyst": true}

// maybePlanPreflight runs the procedure planner before a top-level turn. It (1)
// decomposes the request into ordered steps, (2) — for a multi-step plan — has
// the council audit the procedure before any work, (3) registers the steps as the
// session's todos (the council contract), (4) executes each step with its own
// strategy (solo|parallel|scout, scout being adaptive), and (5) injects the
// gathered findings so the main agent starts with them. Best-effort throughout:
// any failure degrades to solo (the normal path) and never blocks the turn.
// It returns true if it injected explorer findings — i.e. the planner did real
// investigation this turn — so the caller seeds the loop's "used tools" flag and
// the termination council still convenes even if the main agent only synthesizes
// the findings (no tools of its own).
func (a *App) maybePlanPreflight(ctx context.Context, s session.Session) bool {
	if !a.cfg.Planner {
		return false
	}
	spec, ok := a.cfg.Agents[plannerAgent]
	if !ok {
		return false // planner not configured
	}
	prompt := a.lastUserPrompt(ctx, s.ID)
	if strings.TrimSpace(prompt) == "" {
		return false
	}
	a.setStage(s.ID, stagePlan) // tag pre-flight planning events as the plan stage (D15)

	plan := a.runPlanner(ctx, spec, s, prompt, "")
	steps := sanitizeSteps(plan)
	if len(steps) == 0 {
		a.emitPhase(s.ID, "plan", "solo", strings.TrimSpace(plan.Reason)) // ran, judged single-area
		return false                                                      // solo — the default, cheap path
	}

	// Plan audit (D17): a multi-step procedure is reviewed by the council BEFORE it
	// runs. Suppressed in workflow mode (the deterministic engine owns staging) and
	// when no council is configured.
	if len(steps) >= 2 && a.cfg.Council != nil && !a.cfg.Workflow {
		steps = a.runPlanAuditGate(ctx, s, spec, prompt, steps)
		if len(steps) == 0 {
			return false
		}
		// (a single remaining step is fine — nothing to fan out, but solo work follows)
	}

	a.registerPlanTodos(s.ID, steps)
	a.emitPhase(s.ID, "plan", planSummary(steps), strings.TrimSpace(plan.Reason))

	findings := a.executeSteps(ctx, s, steps)
	if strings.TrimSpace(findings) == "" {
		return false
	}
	a.injectPlannerFindings(ctx, s.ID, findings)
	return true
}

// runPlanner does a single tool-free LLM call on the planner's own provider and
// parses the procedure from the reply. revise is non-empty on a re-plan after a
// council plan-audit asked for changes. Returns a zero planResult on any error.
func (a *App) runPlanner(ctx context.Context, spec AgentSpec, s session.Session, prompt, revise string) planResult {
	sys := spec.System + "\n\n# Repository (top level)\n" + repoMap(s.Workdir) + "\n\n" + plannerContract
	if dir := langDirective(prompt); dir != "" {
		sys = dir + " Write the JSON \"reason\" value in that language.\n\n" + sys
	}
	user := prompt
	if strings.TrimSpace(revise) != "" {
		// Re-plan: fold the council's revise feedback into the request.
		user = prompt + "\n\n# Council review of your previous plan (address this and re-plan):\n" + revise
	}
	model := s.Model.Model
	if spec.Model != (session.ModelRef{}) {
		model = spec.Model.Model
	}
	req := port.ChatRequest{
		Model:    model,
		System:   sys,
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
	}
	stream, err := a.providerFor(spec).StreamChat(ctx, req)
	if err != nil {
		return planResult{}
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	return parsePlan(b.String())
}

// plannerContract instructs the planner to emit an ordered procedure with a
// per-step execution strategy, not a solo/parallel boolean.
const plannerContract = "Plan the PROCEDURE to handle the request: an ordered, minimal list of steps, each with how to execute it.\n" +
	"ORDER matters — lay the steps out logically: first locate/scout what is actually relevant, then investigate it, " +
	"then any step that builds on earlier findings. A simple request is a single step. Do NOT pad the plan with broad, " +
	"unrelated area-splits — every step must serve THIS request.\n\n" +
	"Each step has a \"strategy\":\n" +
	"- \"solo\": the main agent does it directly (no explorer). Use for anything that writes/edits, or that needs full context.\n" +
	"- \"parallel\": independent read-only investigations you ALREADY know are relevant — give \"groups\" (each {agent, focus, question}).\n" +
	"- \"scout\": you DON'T yet know the work-list — give \"discover\" (the list to produce, SCOPED TO WHAT THE TASK NEEDS — " +
	"e.g. for a bug hunt, the source files/packages in scope, NOT tangential files like docs) and \"each\" (what to find " +
	"out about every item); a read-only explorer lists them, then one explorer runs per item in parallel.\n\n" +
	"Explorers are READ-ONLY (agent ∈ explore|locator|analyst); never use them to write. " +
	"Reply with ONLY a JSON object, no prose:\n" +
	`{"reason":"one sentence","steps":[{"title":"...","strategy":"solo|parallel|scout","groups":[{"agent":"explore","focus":"...","question":"..."}],"agent":"explore","discover":"...","each":"..."}]}`

// parsePlan extracts the first {...} JSON object from text (models often wrap it
// in prose or fences) and unmarshals it. Invalid → zero planResult.
func parsePlan(text string) planResult {
	i := strings.IndexByte(text, '{')
	j := strings.LastIndexByte(text, '}')
	if i < 0 || j <= i {
		return planResult{}
	}
	var p planResult
	if json.Unmarshal([]byte(text[i:j+1]), &p) != nil {
		return planResult{}
	}
	return p
}

// sanitizeSteps enforces guardrails: valid strategies, read-only explorers, a
// usable shape per strategy, and a capped step count. A "solo" step is kept (it
// structures the procedure / todos) even though it dispatches nothing.
func sanitizeSteps(p planResult) []planStep {
	var out []planStep
	for _, st := range p.Steps {
		st.Title = strings.TrimSpace(st.Title)
		st.Strategy = strings.ToLower(strings.TrimSpace(st.Strategy))
		switch st.Strategy {
		case "parallel":
			var g []planGroup
			for _, x := range st.Groups {
				if strings.TrimSpace(x.Question) == "" {
					continue
				}
				if !readOnlyExplorers[x.Agent] {
					x.Agent = "explore"
				}
				g = append(g, x)
				if len(g) == maxPlanGroups {
					break
				}
			}
			if len(g) == 0 {
				continue // parallel with no usable groups is meaningless
			}
			st.Groups = g
		case "scout":
			if strings.TrimSpace(st.Discover) == "" {
				continue
			}
			if !readOnlyExplorers[st.Agent] {
				st.Agent = "explore"
			}
		case "solo":
			// keep as-is
		default:
			continue // unknown strategy → drop
		}
		if st.Title == "" {
			st.Title = st.Strategy + " step"
		}
		out = append(out, st)
		if len(out) == maxPlanSteps {
			break
		}
	}
	return out
}

// runPlanAuditGate has the council review the PROCEDURE before it runs (D17). It
// returns the procedure to execute — the original (approved or force-approved) or
// a revised one. The pure tally is reused via the council adapter with Phase=plan;
// there is no diff/report/signals, and revise feedback drives a re-plan (it is NOT
// injected into the main session). It has its own bounded rounds.
func (a *App) runPlanAuditGate(ctx context.Context, s session.Session, spec AgentSpec, prompt string, steps []planStep) []planStep {
	sid := s.ID
	actor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	a.setStage(sid, stageCouncil)
	members := a.cfg.CouncilMembers
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	labels := make([]string, len(members))
	for i, m := range members {
		labels[i] = m.Name
	}
	// Consensus rule — the same one the termination gate uses (no special quorum:1
	// relaxation): the plan audit is a real consensus. planMemberSystem already
	// revises only for a concrete flaw, so majority converges.
	rule := a.cfg.CouncilRule
	if rule == "" {
		rule = council.DefaultRule
	}
	// The plan audit shares the termination gate's round cap (CouncilMaxRounds,
	// default 3) rather than a shorter hardcoded limit: round 1 often surfaces a
	// concrete fix that round 2 still hasn't fully absorbed, so a too-small cap
	// force-proceeds on a plan that one more round would have converged.
	maxRounds := a.cfg.CouncilMaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}

	for round := 1; round <= maxRounds; round++ {
		if ctx.Err() != nil {
			return steps
		}
		cd, _ := json.Marshal(event.CouncilConvenedData{
			Round: round, Phase: "plan", Members: labels, Rule: string(rule),
			Task: prompt, Plan: renderSteps(steps),
		})
		a.appendFact(ctx, sid, event.TypeCouncilConvened, actor, cd)
		for _, m := range members {
			ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: round, Member: m.Name, State: "asking"})
			a.publishTransient(sid, event.TypeCouncilDeliberating, actor, ld)
		}

		delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
			Round: round, Phase: "plan", Task: prompt, Plan: renderSteps(steps),
			Members: members, Rule: rule, DefaultModel: s.Model.Model,
		})
		if err != nil { // a gate failure must not block the turn → proceed with the plan
			dd, _ := json.Marshal(event.CouncilDecidedData{Round: round, Phase: "plan", Decision: string(council.Done), Note: "plan council unavailable: " + err.Error()})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			return steps
		}
		for _, v := range delib.Verdicts {
			vd, _ := json.Marshal(event.CouncilVerdictData{
				Round: round, Phase: "plan", Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
				Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback,
			})
			a.appendFact(ctx, sid, event.TypeCouncilVerdict, actor, vd)
		}

		if delib.Decision == council.Done { // approve
			a.storePlanCriteria(ctx, s, delib.Criteria) // the contract for the termination gate
			dd, _ := json.Marshal(event.CouncilDecidedData{Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown, Criteria: delib.Criteria})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			return steps
		}

		// revise — but stop if the cap is hit or there is no actionable feedback.
		fb := strings.TrimSpace(delib.Feedback)
		if round >= maxRounds || fb == "" {
			a.storePlanCriteria(ctx, s, delib.Criteria) // proceeding with this plan → keep its criteria
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown,
				Note: fmt.Sprintf("plan unresolved after %d round(s) — proceeding", round), Criteria: delib.Criteria,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			return steps
		}
		dd, _ := json.Marshal(event.CouncilDecidedData{Round: round, Phase: "plan", Decision: string(council.Continue), Tally: delib.Breakdown, Feedback: fb})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)

		// Re-plan with the feedback folded in.
		a.setStage(sid, stagePlan)
		next := sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, fb))
		a.setStage(sid, stageCouncil)
		if len(next) == 0 {
			// Re-plan failed → keep the prior plan; its criteria (this round's) are the
			// contract we proceed with.
			a.storePlanCriteria(ctx, s, delib.Criteria)
			return steps
		}
		steps = next
	}
	return steps
}

// renderSteps formats the procedure for the council to audit and for the todos.
func renderSteps(steps []planStep) string {
	var b strings.Builder
	for i, st := range steps {
		fmt.Fprintf(&b, "%d. [%s] %s", i+1, st.Strategy, st.Title)
		switch st.Strategy {
		case "scout":
			fmt.Fprintf(&b, " (discover: %s; each: %s)", st.Discover, st.Each)
		case "parallel":
			fmt.Fprintf(&b, " (%d investigations)", len(st.Groups))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// planSummary is a short status detail for the plan phase event.
func planSummary(steps []planStep) string {
	parts := make([]string, len(steps))
	for i, st := range steps {
		parts[i] = st.Strategy
	}
	return fmt.Sprintf("%d steps: %s", len(steps), strings.Join(parts, "→"))
}

// registerPlanTodos seeds the session plan with the procedure's steps so the TUI
// shows one plan and the council reads it as the contract. The main agent takes
// these over and updates them (see injectPlannerFindings).
func (a *App) registerPlanTodos(sid session.SessionID, steps []planStep) {
	td := make([]session.Todo, 0, len(steps))
	for _, st := range steps {
		td = append(td, session.Todo{Content: st.Title, Status: "pending"})
	}
	a.SetTodos(sid, td)
}

// executeSteps runs each step by its strategy, accumulating explorer findings.
// A per-turn explorer budget caps total dispatch; a step that can't dispatch
// (solo, or a scout/parallel that yields nothing) degrades to "the main agent
// handles it" without aborting the procedure.
func (a *App) executeSteps(ctx context.Context, s session.Session, steps []planStep) string {
	budget := maxPlanExplorers
	var out []string
	for _, st := range steps {
		if budget <= 0 || ctx.Err() != nil {
			break
		}
		var groups []planGroup
		switch st.Strategy {
		case "parallel":
			groups = capGroups(st.Groups, &budget)
		case "scout":
			groups = a.scoutGroups(ctx, s, st, &budget)
		default: // solo → main agent does it; nothing to dispatch
			continue
		}
		if len(groups) == 0 {
			continue // per-step degrade
		}
		if f := strings.TrimSpace(a.runExplorers(ctx, s, groups)); f != "" {
			out = append(out, "### "+st.Title+"\n"+f)
		}
	}
	return strings.Join(out, "\n\n")
}

// capGroups trims a parallel step's groups to the remaining per-turn budget.
func capGroups(groups []planGroup, budget *int) []planGroup {
	if len(groups) > *budget {
		groups = groups[:*budget]
	}
	*budget -= len(groups)
	return groups
}

// scoutGroups runs the scout: one read-only explorer produces a work-list, then
// each item becomes a parallel investigation. This is the adaptive scout→fanout —
// the fan-out targets are discovered at runtime, not pre-planned.
func (a *App) scoutGroups(ctx context.Context, s session.Session, st planStep, budget *int) []planGroup {
	agent := st.Agent
	if !readOnlyExplorers[agent] {
		agent = "explore"
	}
	if *budget <= 0 {
		return nil
	}
	listPrompt := fmt.Sprintf("List %s. Output ONLY the items, one per line — no prose, no numbering, no bullets, no markdown.", st.Discover)
	r := a.spawn(ctx, s, 0, port.SpawnRequest{Agent: agent, Prompt: listPrompt})
	*budget-- // the scout itself counts
	if r.Err != "" {
		return nil
	}
	items := parseList(r.Text)
	each := strings.TrimSpace(st.Each)
	if each == "" {
		each = "Investigate this item (read-only)"
	}
	var groups []planGroup
	for _, it := range items {
		if *budget <= 0 || len(groups) == maxPlanGroups {
			break
		}
		groups = append(groups, planGroup{Agent: agent, Focus: it, Question: each + "\n\nItem: " + it})
		*budget--
	}
	return groups
}

// parseList turns a scout's free-text reply into a clean work-list: one item per
// line, stripping numbering/bullets/fences and blank or prose-like lines.
func parseList(text string) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "```") {
			continue
		}
		ln = strings.TrimLeft(ln, "-*•0123456789.) \t")
		ln = strings.TrimSpace(ln)
		if ln == "" || len(ln) > 200 || strings.Contains(ln, " ") && len(strings.Fields(ln)) > 12 {
			continue // skip prose-y lines; work-list items are short tokens/paths/names
		}
		out = append(out, ln)
		if len(out) == maxPlanGroups {
			break
		}
	}
	return out
}

// runExplorers dispatches the groups as read-only subagents concurrently and
// returns their findings concatenated in a stable order.
func (a *App) runExplorers(ctx context.Context, s session.Session, groups []planGroup) string {
	type res struct {
		i    int
		text string
	}
	results := make([]res, 0, len(groups))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Add(1)
		go func(i int, g planGroup) {
			defer wg.Done()
			prompt := fmt.Sprintf("Investigate (read-only): %s\n\n%s", g.Focus, g.Question)
			r := a.spawn(ctx, s, 0, port.SpawnRequest{Agent: g.Agent, Prompt: prompt})
			text := r.Text
			if r.Err != "" {
				text = "(failed: " + r.Err + ")"
			}
			mu.Lock()
			results = append(results, res{i, fmt.Sprintf("## %s\n%s", g.Focus, strings.TrimSpace(text))})
			mu.Unlock()
		}(i, g)
	}
	wg.Wait()
	sort.Slice(results, func(a, b int) bool { return results[a].i < results[b].i })
	parts := make([]string, len(results))
	for i, r := range results {
		parts[i] = r.text
	}
	return strings.Join(parts, "\n\n")
}

// injectPlannerFindings appends the explorers' combined findings as a system
// message so the main agent begins with them, and hands over the plan todos.
func (a *App) injectPlannerFindings(ctx context.Context, sid session.SessionID, findings string) {
	text := "# Investigation findings (from the explorer subagents you just dispatched)\n\n" + findings +
		"\n\n---\nThese are the results of YOUR OWN read-only explorers — trust them as your primary " +
		"source and SYNTHESIZE from them directly. Do NOT re-read or re-investigate what is already " +
		"covered above; open a file again only if you must quote or modify an exact line. " +
		"A plan (todos) has been set for this task — CONTINUE and update those todos as you go; do not replace them wholesale. " +
		"Proceed with the task."
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "planner"}, pd)
}

// lastUserPrompt returns the text of the most recent user-submitted prompt.
func (a *App) lastUserPrompt(ctx context.Context, sid session.SessionID) string {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return ""
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == event.TypePromptSubmitted && evs[i].Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(evs[i].Data, &d) == nil {
				return joinPartText(d.Parts)
			}
		}
	}
	return ""
}

func joinPartText(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind == session.PartText {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// repoMap lists the workdir's top-level entries (dirs marked) to ground the
// planner without an expensive scan. Bounded and best-effort.
func repoMap(workdir string) string {
	ents, err := os.ReadDir(workdir)
	if err != nil {
		return "(unavailable)"
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
		if len(names) == 40 {
			break
		}
	}
	return strings.Join(names, " ")
}
