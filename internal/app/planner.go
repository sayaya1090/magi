package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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

	// explorerTimeout caps each read-only planner explorer well under the 5m subagent
	// hard cap, so a single explorer chasing a bad target can't stall the step (which
	// waits for all explorers) for the full SubagentTimeout.
	explorerTimeout = 3 * time.Minute
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
	Strategy string      `json:"strategy"`         // solo | parallel | scout | delegate
	Groups   []planGroup `json:"groups,omitempty"` // parallel: explorers to fan out
	// scout (adaptive): discover a work-list at runtime, then fan out one explorer
	// per item — this is what lets "list the docs, then read each in parallel" be
	// expressed without the planner knowing the list in advance.
	Agent    string `json:"agent,omitempty"`    // scout+per-item explorer (read-only); OR the delegate's executor
	Discover string `json:"discover,omitempty"` // what list to produce, e.g. "the markdown files under docs/"
	Each     string `json:"each,omitempty"`     // what to find out about each discovered item
	// delegate (recursive execution): hand a large, INDEPENDENT sub-task to a capable
	// sub-agent that plans and carries it out at its own level (unlike the read-only
	// explorers, this one WRITES). Task is the full instruction; Agent names the
	// executor (a configured write-capable agent). Serialized — never parallel — so
	// concurrent writes can't race the council's change capture.
	Task string `json:"task,omitempty"`
}

// planResult is the planner's procedure: an ordered list of steps.
type planResult struct {
	Steps  []planStep `json:"steps"`
	Reason string     `json:"reason"`
	// EstimatedSteps is the planner's guess at how many LOOP STEPS (tool calls)
	// the whole task will take. Advisory only: it feeds the volatile budget line
	// as a pacing reference ("~N expected") and NEVER lowers the hard ceiling —
	// weak models misestimate effort routinely, and a wrong hard cap would cut
	// off genuinely progressing work (the top measured bench failure).
	EstimatedSteps int `json:"estimated_steps"`
}

// readOnlyExplorers are the only agents the planner may dispatch — investigation
// is read-only, so there are no file conflicts and nothing to fabricate-then-write.
var readOnlyExplorers = map[string]bool{"explore": true, "locator": true, "analyst": true}

// producesFiles reports whether an agent authors file deliverables (has edit/write),
// as opposed to a read-only explorer or a run-only verifier. It gates both preflight
// eligibility (only a producing agent benefits from decompose-then-investigate/delegate)
// and which agents may be a delegate step's executor. Deliberately keyed off write/edit,
// NOT bash: a tester/verifier holds bash to RUN checks but must never re-plan (it would
// mutate state during the independent verification pass) nor be handed a build task —
// keying off bash would wrongly sweep it in.
func producesFiles(spec AgentSpec) bool {
	return spec.allows("edit") || spec.allows("write")
}

// delegatableAgents lists the configured agents (except the planner itself) that can
// execute a delegated sub-task, sorted for a stable prompt. Empty means delegate is
// unavailable — the planner is told to use solo/parallel/scout only.
func (a *App) delegatableAgents() []string {
	var out []string
	for name := range a.cfg.Agents {
		if name == plannerAgent {
			continue
		}
		if spec, ok := a.resolveAgentSpec(name); ok && producesFiles(spec) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// delegateAgentName validates a delegate step's requested executor: it must be a
// configured, execute-capable agent. Returns ("", false) when it isn't, so the step
// degrades to solo (the main agent handles that work) rather than dispatching to a
// bogus or read-only agent.
func (a *App) delegateAgentName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == plannerAgent {
		return "", false
	}
	spec, ok := a.resolveAgentSpec(name)
	if !ok || !producesFiles(spec) {
		return "", false
	}
	return name, true
}

// planEligible gates the recursive pre-flight planner (D17): plan only for an agent
// that PRODUCES a deliverable (a read-only explorer/reviewer is a leaf — it never
// re-plans), only while below the recursion cap, and never in workflow mode (the
// deterministic engine owns staging there). This is the single guard that lets a
// delegated sub-task re-plan at its own level while a weak model's tree stays bounded.
func (a *App) planEligible(agent AgentSpec, depth int) bool {
	return a.cfg.Planner && !a.cfg.Workflow && depth < a.cfg.MaxPlanDepth && producesFiles(agent)
}

// maybePlanPreflight runs the procedure planner before a top-level turn. It (1)
// decomposes the request into ordered steps, (2) — for a multi-step plan — has
// the council audit the procedure before any work, (3) registers the steps as the
// session's todos (the council contract), (4) executes each step with its own
// strategy (solo|parallel|scout, scout being adaptive), and (5) injects the
// gathered findings so the main agent starts with them. Best-effort throughout:
// any failure degrades to solo (the normal path) and never blocks the turn.
// It returns planned=true when it injected findings (the planner did real work this
// turn) so the caller seeds the loop's "used tools" flag and the termination council
// still convenes. delegated=true when a delegate step actually carried out WRITE work
// via a sub-agent: those writes land in the child's guard, not the parent's, so the
// caller must seed usedMutator to force the parent's depth-0 verification (review-gate
// tester / council) to inspect and verify the MERGED working tree.
func (a *App) maybePlanPreflight(ctx context.Context, s session.Session, depth int) (planned, delegated bool) {
	if !a.cfg.Planner {
		return false, false
	}
	spec, ok := a.cfg.Agents[plannerAgent]
	if !ok {
		return false, false // planner not configured
	}
	prompt := a.lastUserPrompt(ctx, s.ID)
	if strings.TrimSpace(prompt) == "" {
		return false, false
	}
	a.setStage(s.ID, stagePlan) // tag pre-flight planning events as the plan stage (D15)

	plan := a.runPlanner(ctx, spec, s, prompt, "")
	a.storeStepEstimate(s.ID, plan.EstimatedSteps) // advisory pacing reference, solo or not
	steps := sanitizeSteps(plan)
	if len(steps) == 0 {
		a.emitPhase(s.ID, "plan", "solo", strings.TrimSpace(plan.Reason)) // ran, judged single-area
		return false, false                                               // solo — the default, cheap path
	}

	// Plan audit (D17): a multi-step procedure is reviewed by the council BEFORE it
	// runs. Suppressed in workflow mode (the deterministic engine owns staging) and
	// when no council is configured.
	if len(steps) >= 2 && a.cfg.Council != nil && !a.cfg.Workflow {
		steps = a.runPlanAuditGate(ctx, s, spec, prompt, steps)
		if len(steps) == 0 {
			return false, false
		}
		// (a single remaining step is fine — nothing to fan out, but solo work follows)
	}

	a.registerPlanTodos(ctx, s.ID, steps)
	a.emitPhase(s.ID, "plan", planSummary(steps), strings.TrimSpace(plan.Reason))

	findings, delegated := a.executeSteps(ctx, s, steps, depth)
	if strings.TrimSpace(findings) == "" {
		return false, false
	}
	a.injectPlannerFindings(ctx, s.ID, findings, delegated)
	return true, delegated
}

// runPlanner does a single tool-free LLM call on the planner's own provider and
// parses the procedure from the reply. revise is non-empty on a re-plan after a
// council plan-audit asked for changes. Returns a zero planResult on any error.
func (a *App) runPlanner(ctx context.Context, spec AgentSpec, s session.Session, prompt, revise string) planResult {
	sys := spec.System + "\n\n# Repository (top level)\n" + repoMap(s.Workdir) + "\n\n" + plannerContract
	if names := a.delegatableAgents(); len(names) > 0 {
		sys += "\n\nDelegate executors available (use one as a delegate step's \"agent\"): " + strings.Join(names, ", ") + "."
	} else {
		sys += "\n\nNo delegate executors are configured — do NOT use the \"delegate\" strategy; use solo/parallel/scout only."
	}
	if dir := langDirective(prompt); dir != "" {
		sys = dir + " Write the JSON \"reason\" value in that language.\n\n" + sys
	}
	// Ground the plan in the conversation, not just the latest sentence: a follow-up
	// like "now change it to two newlines" is meaningless without the prior turns
	// (which file, what change). The main loop sends full history to the agent; the
	// planner must see a recent window too, or it plans for a bare sentence out of
	// context (e.g. "scout the whole project for files with single newlines").
	evs, _ := a.store.Read(ctx, s.ID, 0)
	msgs := plannerWindow(reconstruct(evs))
	if strings.TrimSpace(revise) != "" {
		// Re-plan: append the council's revise feedback as a final instruction.
		msgs = append(msgs, session.Message{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText,
			Text: "# Council review of your previous plan (address this and re-plan):\n" + revise}}})
	}
	if len(msgs) == 0 { // defensive: never call with an empty conversation
		msgs = []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: prompt}}}}
	}
	model := s.Model.Model
	if spec.Model != (session.ModelRef{}) {
		model = spec.Model.Model
	}
	req := port.ChatRequest{
		Model:    model,
		System:   sys,
		Messages: msgs,
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

// recentTranscript renders a compact, bounded tail of the conversation as plain text
// — for grounding the plan-audit council, which otherwise judges the plan against the
// bare instruction. Text parts are included (truncated); tool calls are summarized to
// their name; the whole is capped so it can't dominate a member's evidence prompt.
func recentTranscript(msgs []session.Message, budget int) string {
	trunc := func(s string, n int) string {
		s = strings.Join(strings.Fields(s), " ")
		if r := []rune(s); len(r) > n {
			return string(r[:n]) + "…"
		}
		return s
	}
	var lines []string
	for _, m := range msgs {
		who := string(m.Role)
		for _, p := range m.Parts {
			switch p.Kind {
			case session.PartText:
				if t := strings.TrimSpace(p.Text); t != "" {
					lines = append(lines, who+": "+trunc(t, 200))
				}
			case session.PartToolCall:
				lines = append(lines, who+": [tool "+p.ToolCall.Name+"]")
			}
		}
	}
	out := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if out != "" && len(out)+len(lines[i]) > budget {
			break
		}
		out = lines[i] + "\n" + out
	}
	return strings.TrimRight(out, "\n")
}

// plannerWindow returns a bounded tail of the conversation for the planner: enough
// recent turns to ground a follow-up request, without resending a whole long session
// on a cheap pre-flight call. Whole messages are kept from the end within a byte
// budget, always including at least the final (current) message.
func plannerWindow(msgs []session.Message) []session.Message {
	const budget = 8000 // ~a few turns; the planner is a single tool-free call
	if len(msgs) == 0 {
		return msgs
	}
	total, start := 0, len(msgs)-1
	for i := len(msgs) - 1; i >= 0; i-- {
		total += msgLen(msgs[i])
		start = i
		if total >= budget {
			break
		}
	}
	return msgs[start:]
}

// msgLen approximates a message's size (for windowing) via its JSON encoding, which
// captures text, tool-call args, and results alike.
func msgLen(m session.Message) int {
	b, _ := json.Marshal(m)
	return len(b)
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
	"out about every item); a read-only explorer lists them, then one explorer runs per item in parallel.\n" +
	"- \"delegate\": hand a LARGE, INDEPENDENT chunk of the WORK (that WRITES/BUILDS/RUNS/FIXES something) to a sub-agent " +
	"that plans and carries it out on its own — give \"task\" (the full self-contained instruction) and \"agent\" (the executor). " +
	"Use this ONLY when the task genuinely splits into big, mostly-independent build/fix units (e.g. two separate subcommands, " +
	"a component plus its tests). Decompose CONSERVATIVELY: a small, quick, or tightly-coupled piece of work is a \"solo\" step, " +
	"NOT a delegate — do NOT shatter one coherent change into many tiny delegates. Prefer the fewest units that are each " +
	"worth handing off whole. If no executor agent is available (see below), don't use delegate.\n\n" +
	"Explorers are READ-ONLY (agent ∈ explore|locator|analyst); never use them to write. " +
	"Also give \"estimated_steps\": your honest guess at the TOTAL number of tool calls the whole task needs " +
	"(a one-file tweak ~5, a feature with tests ~30, a big build/debug ~100). It is pacing guidance only — never a limit.\n" +
	"Reply with ONLY a JSON object, no prose:\n" +
	`{"reason":"one sentence","estimated_steps":12,"steps":[{"title":"...","strategy":"solo|parallel|scout|delegate","groups":[{"agent":"explore","focus":"...","question":"..."}],"agent":"explore","discover":"...","each":"...","task":"..."}]}`

// parsePlan extracts the first BALANCED {...} JSON object from text and unmarshals
// it. Models wrap the object in prose/fences, and weak local models often append
// trailing prose containing a stray '}' — the old first-'{'/last-'}' span then
// over-captured and failed to parse, silently degrading a valid multi-step plan to
// the solo path. The balanced scan (string/escape-aware) takes just the object.
func parsePlan(text string) planResult {
	js := firstBalancedObject(text)
	if js == "" {
		return planResult{}
	}
	var p planResult
	if json.Unmarshal([]byte(js), &p) != nil {
		return planResult{}
	}
	return p
}

// firstBalancedObject returns the first balanced {...} object in s, respecting
// strings and escapes (braces inside string values don't confuse it), or "".
func firstBalancedObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		switch {
		case esc:
			esc = false
		case ch == '\\' && inStr:
			esc = true
		case ch == '"':
			inStr = !inStr
		case inStr:
			// inside a string literal — ignore structural chars
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// stripStrategyTag removes a leading "[strategy]" tag the planner model sometimes
// echoes into the title (e.g. "[scout] discover docs"). renderSteps already prefixes
// the strategy, so without this the tag shows twice ("[scout] [scout] ..."). Only a
// leading bracket whose contents are a known strategy is removed, so a title that
// genuinely starts with brackets is left intact.
func stripStrategyTag(title string) string {
	if !strings.HasPrefix(title, "[") {
		return title
	}
	if tag, rest, ok := strings.Cut(title[1:], "]"); ok {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "solo", "parallel", "scout":
			return strings.TrimSpace(rest)
		}
	}
	return title
}

// sanitizeSteps enforces guardrails: valid strategies, read-only explorers, a
// usable shape per strategy, and a capped step count. A "solo" step is kept (it
// structures the procedure / todos) even though it dispatches nothing.
func sanitizeSteps(p planResult) []planStep {
	var out []planStep
	for _, st := range p.Steps {
		st.Strategy = strings.ToLower(strings.TrimSpace(st.Strategy))
		st.Title = stripStrategyTag(strings.TrimSpace(st.Title))
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
		case "delegate":
			if strings.TrimSpace(st.Task) == "" {
				continue // a delegate with no work instruction is meaningless
			}
			st.Agent = strings.TrimSpace(st.Agent) // executor validated at dispatch (executeSteps)
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

	// Ground the audit in the conversation: a follow-up plan ("change it to two
	// newlines") is unjudgeable against the bare instruction alone, so the members
	// thrash (revise → no consensus). Prepend a compact recent transcript.
	auditTask := prompt
	if evs, err := a.store.Read(ctx, sid, 0); err == nil {
		if cx := recentTranscript(reconstruct(evs), 1500); cx != "" {
			auditTask = "# Recent conversation (for context)\n" + cx + "\n\n# Current request to plan for\n" + prompt
		}
	}

	for round := 1; round <= maxRounds; round++ {
		if ctx.Err() != nil {
			return steps
		}
		cd, _ := json.Marshal(event.CouncilConvenedData{
			Round: round, Phase: "plan", Members: labels, Rule: string(rule),
			Task: auditTask, Plan: renderSteps(steps),
		})
		a.appendFact(ctx, sid, event.TypeCouncilConvened, actor, cd)
		for _, m := range members {
			ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: round, Member: m.Name, State: "asking"})
			a.publishTransient(sid, event.TypeCouncilDeliberating, actor, ld)
		}

		delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
			Round: round, Phase: "plan", Task: auditTask, Plan: renderSteps(steps),
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
				Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback, Severity: v.Severity,
			})
			a.appendFact(ctx, sid, event.TypeCouncilVerdict, actor, vd)
		}

		// Severity-gated decision (D17): only a CRITICAL revision blocks the agent from
		// starting work. warn/info concerns are ACCEPTED as advice — injected so the
		// executor heeds them during the turn, and the council's completion criteria (which
		// the termination gate verifies) still apply — instead of looping the plan, which on
		// a slow model burned the whole budget before any work. critical vetoes (one member
		// suffices) so a genuine plan flaw still stops the agent.
		critical := council.HasCriticalRevision(delib.Verdicts)
		advice := strings.TrimSpace(council.AdvisoryFeedback(delib.Verdicts))

		if !critical { // approve, possibly carrying non-blocking advice
			a.storePlanCriteria(ctx, s, delib.Criteria) // the contract for the termination gate
			note := ""
			if advice != "" {
				a.injectCouncilAdvice(ctx, s.ID, advice, true) // accepted: the executor heeds it
				note = "plan approved with advisory notes (non-blocking)"
				if a.cfg.CouncilPlanAbsorb { // option B: fold the advice into the plan now
					if next := sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, advice)); len(next) > 0 {
						steps = next
					}
				}
			}
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done),
				Tally: delib.Breakdown, Note: note, Criteria: delib.Criteria,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			return steps
		}

		// critical → block. Stop if the cap is hit or there is no actionable feedback.
		fb := strings.TrimSpace(council.CriticalFeedback(delib.Verdicts))
		if round >= maxRounds || fb == "" {
			a.storePlanCriteria(ctx, s, delib.Criteria) // proceeding with this plan → keep its criteria
			// Proceeding PAST an unresolved critical: hand the executor that critical
			// concern (plus any advice) so it can still try to address it — don't bury it
			// in a note only.
			if carry := strings.TrimSpace(fb + "\n\n" + advice); carry != "" {
				a.injectCouncilAdvice(ctx, s.ID, carry, false)
			}
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown,
				Note: fmt.Sprintf("critical plan concern unresolved after %d round(s) — proceeding", round), Criteria: delib.Criteria,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			return steps
		}
		dd, _ := json.Marshal(event.CouncilDecidedData{Round: round, Phase: "plan", Decision: string(council.Continue), Tally: delib.Breakdown, Feedback: fb})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)

		// Re-plan with the blocking feedback folded in (one retry — local models are flaky
		// and an empty/unparseable reply shouldn't silently drop the revision).
		a.setStage(sid, stagePlan)
		next := sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, fb))
		if len(next) == 0 {
			next = sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, fb))
		}
		a.setStage(sid, stageCouncil)
		if len(next) == 0 {
			// Re-plan failed → proceed with the prior plan, but say so (don't silently
			// run a plan the council just rejected). Keep this round's criteria.
			a.storePlanCriteria(ctx, s, delib.Criteria)
			dd2, _ := json.Marshal(event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown,
				Note: "re-plan failed — proceeding with the prior plan", Criteria: delib.Criteria,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd2)
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
func (a *App) registerPlanTodos(ctx context.Context, sid session.SessionID, steps []planStep) {
	td := make([]session.Todo, 0, len(steps))
	for _, st := range steps {
		td = append(td, session.Todo{Content: st.Title, Status: "pending"})
	}
	a.putTodos(ctx, sid, plannerActor, td)
}

// plannerActor attributes the planner's todo writes (seed + per-step check-off).
var plannerActor = event.Actor{Kind: event.ActorAgent, ID: plannerAgent}

// executeSteps runs each step by its strategy, accumulating explorer findings.
// A per-turn explorer budget caps total dispatch; a step that can't dispatch
// (solo, or a scout/parallel that yields nothing) degrades to "the main agent
// handles it" without aborting the procedure.
func (a *App) executeSteps(ctx context.Context, s session.Session, steps []planStep, depth int) (findings string, delegated bool) {
	budget := maxPlanExplorers
	var out []string
	for i, st := range steps {
		if budget <= 0 || ctx.Err() != nil {
			break
		}
		// delegate: a heavier, write-capable sub-task. Handled inline in this sequential
		// loop (never fanned out concurrently) so its writes can't race the council's
		// change capture — see allParallelSafe. The sub-agent re-plans at depth+1.
		if st.Strategy == "delegate" {
			agentName, ok := a.delegateAgentName(st.Agent)
			if !ok {
				continue // no valid executor → degrade to solo (the main agent does it)
			}
			budget-- // count against the per-turn dispatch budget like an explorer
			a.advanceTo(ctx, s.ID, plannerActor, i)
			r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: delegatePrompt(st)})
			text := strings.TrimSpace(r.Text)
			if r.Err != "" || text == "" {
				// Failed or empty → the sub-task is NOT done. Leave its todo pending so the main
				// agent picks it up, and record it as FAILED (never "(delegated to …)", so the
				// redo-prevention directive can't tell the agent to skip re-doing it).
				note := "the delegated sub-agent returned no result"
				if r.Err != "" {
					note = "the delegated sub-agent errored: " + r.Err
				}
				out = append(out, "### "+st.Title+" (delegate FAILED — do this yourself)\n("+note+"; this sub-task is unfinished)")
				a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
				continue
			}
			out = append(out, "### "+st.Title+" (delegated to "+agentName+")\n"+text)
			delegated = true
			a.completeThrough(ctx, s.ID, plannerActor, i)
			continue
		}
		var groups []planGroup
		switch st.Strategy {
		case "parallel":
			groups = capGroups(st.Groups, &budget)
		case "scout":
			groups = a.scoutGroups(ctx, s, st, &budget, depth)
		default: // solo → main agent does it; nothing to dispatch
			continue
		}
		if len(groups) == 0 {
			continue // per-step degrade
		}
		a.advanceTo(ctx, s.ID, plannerActor, i) // moved on to step i: earlier steps ✓, step i running ◐
		if f := strings.TrimSpace(a.runExplorers(ctx, s, groups, depth)); f != "" {
			out = append(out, "### "+st.Title+"\n"+f)
			a.completeThrough(ctx, s.ID, plannerActor, i) // step i done
		} else {
			a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending") // degraded → don't leave a stuck ◐
		}
	}
	return strings.Join(out, "\n\n"), delegated
}

// delegatePrompt frames a delegate step as a self-contained sub-task instruction.
func delegatePrompt(st planStep) string {
	return st.Task + "\n\n(You are handling ONE independent part of a larger plan. Complete this part fully, " +
		"verify it yourself, and report what you did and how you verified it.)"
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
func (a *App) scoutGroups(ctx context.Context, s session.Session, st planStep, budget *int, depth int) []planGroup {
	agent := st.Agent
	if !readOnlyExplorers[agent] {
		agent = "explore"
	}
	if *budget <= 0 {
		return nil
	}
	listPrompt := fmt.Sprintf("List %s. Output ONLY the items, one per line — no prose, no numbering, no bullets, no markdown. The FIRST line must already be an item: no title, header, preamble (\"Here are…\", \"List of…:\"), or closing remark.", st.Discover)
	r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agent, Prompt: listPrompt})
	*budget-- // the scout itself counts
	if r.Err != "" {
		return nil
	}
	items := parseList(stripReportStatus(r.Text))
	kept := items[:0]
	for _, it := range items {
		if keepScoutItem(s.Workdir, it) {
			kept = append(kept, it)
		}
	}
	items = kept
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

// stripReportStatus drops the leading "STATUS: <WORD>" line that subReport.result
// (app.go) prepends to a subagent's report. A scout's discovery agent files its
// work-list via the report tool, so r.Text arrives report-framed; without this the
// frame line itself ("STATUS: DONE") would be parsed as a bogus work-item. Only the
// exact two-field frame is removed, so a legitimate first item that merely starts
// with "STATUS:" (multi-word) or a path is never touched.
func stripReportStatus(text string) string {
	text = strings.TrimLeft(text, "\n")
	if line, rest, ok := strings.Cut(text, "\n"); ok {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "STATUS: ") && len(strings.Fields(t)) == 2 {
			return rest
		}
	}
	return text
}

// endsWithSentencePunct reports whether s ends in heading/sentence punctuation, which
// marks a header or prose line rather than a work-item (paths/names never do).
func endsWithSentencePunct(s string) bool {
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case ':', '.', '!', '?':
		return true
	}
	return false
}

// keepScoutItem decides whether a parsed discovery line is a real work-item. A real
// file/path/symbol target is a SINGLE TOKEN; a multi-word line is almost always prose —
// a header or sentence the model emitted around its list ("List of files in project
// root and docs directory") that punctuation/field heuristics miss. Keep a multi-word
// line ONLY when it resolves to a real path inside the workdir (a genuine path that
// happens to contain a space). Dropping a multi-word non-path just skips one explorer
// (a benign per-step degrade); keeping one sends an explorer chasing a target that does
// not exist, which flails until its timeout and stalls the whole scout step.
func keepScoutItem(workdir, item string) bool {
	item = strings.Trim(item, "\"'`")
	if item == "" {
		return false
	}
	if !strings.ContainsAny(item, " \t") {
		return true // single token: a path/symbol we can't cheaply validate — keep
	}
	p := filepath.Join(workdir, item)
	root := filepath.Clean(workdir)
	if p != root && !strings.HasPrefix(p, root+string(os.PathSeparator)) {
		return false // escapes the workdir → not a real in-tree item
	}
	_, err := os.Stat(p)
	return err == nil
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
		if ln == "" || len(ln) > 200 {
			continue
		}
		// A work-item is a short token/path/name. A MULTI-WORD line is prose, not an
		// item, when it is long OR ends in sentence/heading punctuation — i.e. a header
		// or preamble the model printed before its list ("List of documentation files:",
		// "Here are the docs.") or a trailing remark. Dispatching one sends an explorer
		// chasing a target that doesn't exist, flailing until the subagent timeout.
		// Single-token items are always kept ("path:line", a "server:" config key).
		if strings.Contains(ln, " ") && (len(strings.Fields(ln)) > 12 || endsWithSentencePunct(ln)) {
			continue
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
func (a *App) runExplorers(ctx context.Context, s session.Session, groups []planGroup, depth int) string {
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
			// Bound each read-only explorer well under the 5m subagent cap: a focused
			// investigation is quick, and one explorer chasing a bad target must not stall
			// the whole step (runExplorers waits for all) for the full SubagentTimeout.
			ectx, ecancel := context.WithTimeout(ctx, explorerTimeout)
			defer ecancel()
			r := a.spawn(ectx, s, depth, port.SpawnRequest{Agent: g.Agent, Prompt: prompt})
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
// injectCouncilAdvice surfaces the plan council's non-blocking (warn/info) advice to
// the agent as a system message, so the executor heeds it during the turn. The advice
// is non-blocking: the plan was approved and the turn proceeds; the completion criteria
// the council derived (verified at the termination gate) remain the contract.
func (a *App) injectCouncilAdvice(ctx context.Context, sid session.SessionID, advice string, approved bool) {
	tail := "The plan council APPROVED your plan but raised the notes above. Incorporate them where they " +
		"improve the result as you carry out the plan — they are not blocking, so proceed with the task."
	if !approved {
		tail = "The plan council could not fully resolve the concerns above within the round cap, but is " +
			"proceeding. Address them as you carry out the plan."
	}
	text := "# Plan review — notes for execution\n\n" + advice + "\n\n---\n" + tail
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "council"}, pd)
}

func (a *App) injectPlannerFindings(ctx context.Context, sid session.SessionID, findings string, delegated bool) {
	text := "# Investigation findings (from the explorer subagents you just dispatched)\n\n" + findings +
		"\n\n---\nThese are the results of YOUR OWN read-only explorers — trust them as your primary " +
		"source and SYNTHESIZE from them directly. Do NOT re-read or re-investigate what is already " +
		"covered above; open a file again only if you must quote or modify an exact line. " +
		"A plan (todos) has been set for this task — CONTINUE and update those todos as you go; do not replace them wholesale. " +
		"Proceed with the task."
	if delegated {
		// Some steps above were CARRIED OUT by delegated sub-agents (marked "(delegated…)"),
		// not just investigated. The parent must integrate/verify — not redo — that work.
		text = "# Investigation findings and completed sub-tasks (from the subagents you just dispatched)\n\n" + findings +
			"\n\n---\nSteps marked \"(delegated to …)\" above were COMPLETED by a sub-agent — the described work is " +
			"already done on disk. Do NOT re-implement them: VERIFY they are correct and INTEGRATE them (run the " +
			"build/tests, reconcile the pieces, fix any gaps between them). Read-only findings above are your " +
			"primary source — synthesize from them; open a file again only to quote or modify an exact line. " +
			"A plan (todos) has been set — CONTINUE and update those todos; do not replace them wholesale. Proceed."
	}
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
