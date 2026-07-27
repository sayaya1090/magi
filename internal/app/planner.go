package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/jsonx"
	"github.com/sayaya1090/magi/internal/port"
)

// plannerAgent is the agent name the pre-flight planner is configured under (its
// system prompt, model, and provider come from cfg.Agents["planner"], so it is
// routable like any other agent, e.g. [routing] planner = "fast").
const plannerAgent = "planner"

const (
	maxPlanGroups    = 5 // explorers per single parallel/scout fan-out
	maxPlanSteps     = 6 // ordered steps the planner may propose
	maxPlanExplorers = 8 // per-turn READ-ONLY explorer spawns (a per-turn budget, not the lifetime MaxAgents)

	// maxPlanWriteSteps is the per-turn budget for WRITE dispatches — every delegate/refine
	// spawn plus their ADaPT retries. It is deliberately SEPARATE from maxPlanExplorers:
	// investigating and executing must not draw on one counter, or a scout that fans out wide
	// leaves the council-approved write steps with nothing to spend, and the tail of the plan
	// silently drops onto the main agent. Sized so a maximum plan (maxPlanSteps) can dispatch
	// every step and still afford a few retries. It is a per-turn shape control, not the real
	// ceiling on spawning: MaxAgents (lifetime) and the spawn soft budget in forceDelegateSteps
	// still bound how much this turn may spawn in total.
	maxPlanWriteSteps = 10

	// explorerTimeout caps each read-only planner explorer well under the 5m subagent
	// hard cap, so a single explorer chasing a bad target can't stall the step (which
	// waits for all explorers) for the full SubagentTimeout.
	explorerTimeout = 3 * time.Minute
)

// planGroup is one independent investigation to parallelize.
type planGroup struct {
	Agent    string `json:"agent"`    // read-only explorer: explore|locator
	Focus    string `json:"focus"`    // short label of the area
	Question string `json:"question"` // what this explorer should find out
}

// flexString is a free-text plan field that tolerates the shapes a model actually emits where the
// schema says string: a plain string, a LIST of strings (it enumerates instead of writing prose),
// or a number. A strict string rejected the WHOLE plan over one field — observed live: a 2271-char,
// otherwise-valid six-step plan discarded because `discover` arrived as an array, costing a full
// re-generation round trip. Same rationale as the tools' flexInt/flexBool. A list is joined rather
// than truncated, because each element is part of what the field says.
type flexString string

func (v *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*v = flexString(s)
		return nil
	}
	var list []string
	if json.Unmarshal(b, &list) == nil {
		*v = flexString(strings.Join(list, "; "))
		return nil
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*v = flexString(n.String())
		return nil
	}
	*v = "" // anything else → unset, never a rejected plan
	return nil
}

// planStep is one ordered step of the procedure plus HOW to execute it (D17).
type planStep struct {
	Title    string      `json:"title"`            // human-facing step (becomes a todo)
	Strategy string      `json:"strategy"`         // solo | parallel | scout | delegate | refine
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
	//
	// refine (hierarchical recursion): a large, NON-independent sub-goal (may depend on
	// earlier steps) stated abstractly. Reuses Task as the sub-goal. Unlike delegate it
	// executes IN-CONTEXT — a child session CLONED from the parent's conversation re-plans
	// it at depth+1, so the sub-goal is worked out with the full context carried forward;
	// on failure the failure is recorded back into the parent context and the node is
	// re-planned locally, escalating to the parent only when local retries are exhausted.
	Task string `json:"task,omitempty"`
}

// UnmarshalJSON reads a step through flexString for its free-text fields, so a model that answers
// a prose field with a LIST (or a number) does not cost the whole plan. Go's decoder aborts the
// entire document on the first type mismatch, so one such field discarded every step alongside it.
func (p *planStep) UnmarshalJSON(b []byte) error {
	// A shadow type is required: naming planStep here would recurse into this method.
	var s struct {
		Title    flexString  `json:"title"`
		Strategy string      `json:"strategy"`
		Groups   []planGroup `json:"groups,omitempty"`
		Agent    string      `json:"agent,omitempty"`
		Discover flexString  `json:"discover,omitempty"`
		Each     flexString  `json:"each,omitempty"`
		Task     flexString  `json:"task,omitempty"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		// A step that arrived as a bare string ("build the parser") is the model writing the plan
		// as a list of titles. Keeping it as a titled step preserves the plan; returning the error
		// would abort the enclosing array and discard every WELL-FORMED step beside it.
		var title string
		if json.Unmarshal(b, &title) == nil && strings.TrimSpace(title) != "" {
			*p = planStep{Title: strings.TrimSpace(title)}
			return nil
		}
		return err
	}
	*p = planStep{
		Title: string(s.Title), Strategy: s.Strategy, Groups: s.Groups, Agent: s.Agent,
		Discover: string(s.Discover), Each: string(s.Each), Task: string(s.Task),
	}
	return nil
}

// planResult is the planner's procedure: an ordered list of steps.
type planResult struct {
	Steps  []planStep `json:"steps"`
	Reason string     `json:"reason"`
	// Contest is the re-planner's rebuttal to a plan-audit concern it believes the TASK does not
	// require or that the plan already satisfies — set instead of complying, and re-judged by the
	// next audit round (planContestEnabled). Empty on a normal plan.
	Contest string `json:"contest,omitempty"`
	// EstimatedSteps is the planner's guess at how many LOOP STEPS (tool calls)
	// the whole task will take. Advisory only: it feeds the volatile budget line
	// as a pacing reference ("~N expected") and NEVER lowers the hard ceiling —
	// weak models misestimate effort routinely, and a wrong hard cap would cut
	// off genuinely progressing work (the top measured bench failure).
	EstimatedSteps int `json:"estimated_steps"`
}

// UnmarshalJSON reads a plan tolerantly for the same reason planStep does: the decoder aborts the
// WHOLE document on the first type mismatch, so a `reason` answered with a list, or an
// `estimated_steps` quoted as "8", discarded every STEP in the plan alongside it — and the planner
// then pays a full re-generation round trip for a plan it had already written correctly.
// `steps` also accepts a single object, which a model emits when its plan has exactly one step.
func (p *planResult) UnmarshalJSON(b []byte) error {
	// A shadow type is required: naming planResult here would recurse into this method.
	var s struct {
		Steps          json.RawMessage `json:"steps"`
		Reason         flexString      `json:"reason"`
		Contest        flexString      `json:"contest,omitempty"`
		EstimatedSteps jsonx.Number    `json:"estimated_steps"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*p = planResult{
		Reason: string(s.Reason), Contest: string(s.Contest), EstimatedSteps: int(s.EstimatedSteps),
	}
	if len(s.Steps) == 0 {
		return nil
	}
	if err := json.Unmarshal(s.Steps, &p.Steps); err == nil {
		return nil
	}
	var one planStep
	if json.Unmarshal(s.Steps, &one) == nil && strings.TrimSpace(one.Title) != "" {
		p.Steps = []planStep{one}
	}
	return nil // an unreadable `steps` leaves the plan empty; the caller already treats that as no plan
}

// readOnlyExplorers are the only agents the planner may fan out — investigation
// is read-only, so there are no file conflicts and nothing to fabricate-then-write.
// These are LOCATE/GATHER roles only. Deep reasoning is deliberately NOT fanned out:
// a fanned-out explorer receives just its focus/question and a clipped goal
// (explorerPrompt), never the parent's full context, so analysis — which depends on
// maximum context — belongs in the full-context main agent (a solo step), not a
// context-starved subagent. The bundled roster carries no write-capable subagent, so
// all authoring stays on the main agent's solo path.
var readOnlyExplorers = map[string]bool{"explore": true, "locator": true}

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
// taskOverride, when non-empty, is the task the plan should decompose instead of the
// session's last user prompt — used when regrounding after a route_interjection so the
// re-plan follows the ADOPTED task (append folds the original goal + the steer's
// constraint) rather than the bare steer text, which alone loses the original intent.
// isTrivialPrompt reports whether a request is simple enough to skip the pre-flight
// planner entirely — one short clause. Skipping avoids a planner LLM round-trip (and the
// plan-audit council it can trigger) on work the main agent finishes in one shot. The
// test is purely structural, no keyword lists: a request is trivial only when it is short
// in both bytes and words and carries no clause-joining punctuation. A coordinated task
// ("refactor auth and update the callers") overruns the word bound or the punctuation,
// so it still gets planned; anything long or multi-line does too.
func isTrivialPrompt(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if p == "" || strings.ContainsAny(p, "\n\r,;") {
		return false // multi-line or multi-clause ⇒ likely multi-part
	}
	if len(p) > 60 || len(strings.Fields(p)) > 6 {
		return false // long enough to plausibly need locating/decomposition
	}
	return true
}

func (a *App) maybePlanPreflight(ctx context.Context, s session.Session, depth, maxSteps int, taskOverride string) (planned, delegated bool) {
	if !a.cfg.Planner {
		return false, false
	}
	spec, ok := a.cfg.Agents[plannerAgent]
	if !ok {
		return false, false // planner not configured
	}
	prompt := strings.TrimSpace(taskOverride)
	if prompt == "" {
		prompt = a.lastUserPrompt(ctx, s.ID)
	}
	if strings.TrimSpace(prompt) == "" {
		return false, false
	}
	// Triviality skip: a single short clause is handled by the main agent in one shot,
	// so the planner round-trip (and the plan-audit council it can trigger) is pure
	// overhead. Only the ordinary path skips — a regrounding re-plan (taskOverride) is a
	// deliberate decomposition we always honor.
	if taskOverride == "" && isTrivialPrompt(prompt) {
		return false, false
	}
	a.setStage(s.ID, stagePlan) // tag pre-flight planning events as the plan stage (D15)

	// Prompt analysis (FRONT step): BEFORE the contract gate and the planner decompose, analyze the
	// REQUEST itself — its identifiers/types/prerequisites and HOW each requirement must be honored
	// (⟨hard⟩ = match verbatim · ⟨example⟩ = reproduce the sample · ⟨semantic⟩ = verify by effect, not a
	// source spelling) — and inject it as a note. Running it FIRST is the point: the contract gate, the
	// planner, and the plan-audit check-author all read the classification. It used to run AFTER the
	// plan was built, so only the executor and the termination council saw it and the check-author —
	// where an over-literal source assertion is born (the kv-store `<key: string>` failure) — never did.
	// Uses only the request text (no plan/repo dependency); plan-based repo exploration is a later step.
	// Best-effort — an empty/failed elicitation injects nothing.
	if specMineEnabled() {
		a.emitToolProgress(s.ID, plannerActor, "", "planner", "analyzing the request (identifiers, types, how each is honored)…")
		if mined := a.elicitSpecMine(ctx, spec, s, prompt); mined != "" {
			a.storeSpecMine(s.ID, mined) // planner, check-author, and termination council share this contract
			_ = a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "planner"}, specMineNote(mined))
		}
	}

	// Contract-first (D-contract): author+review the acceptance contract for the TASK before the
	// planner decomposes it, then feed it in so the plan targets a reviewed contract. TOP-LEVEL ONLY
	// (depth == 0): a delegated worker already received its acceptance checklist from the parent's
	// contract, so re-deriving the whole contract in the worker is redundant AND dangerous — the
	// contract gate is silent side-LLM council calls (no tool activity), and a leased worker sitting
	// in them looks stuck to the supervisor and gets killed in a spawn→kill loop. No-op also when the
	// flag is off, no council is configured, or a contract was already frozen this turn.
	if !a.cfg.Workflow && depth == 0 {
		a.runContractGate(ctx, s, prompt)
	}

	plan := a.runPlanner(ctx, spec, s, prompt, "", replanContext{}, depth, maxSteps, strings.TrimSpace(taskOverride))
	a.storeStepEstimate(s.ID, plan.EstimatedSteps) // advisory pacing reference, solo or not
	steps := guardExpansion(sanitizeSteps(plan), depth, a.cfg.MaxPlanDepth)
	if len(steps) == 0 {
		a.emitPhase(s.ID, "plan", "solo", strings.TrimSpace(plan.Reason)) // ran, judged single-area
		// Solo bypasses the plan audit entirely, so it authors NO deliverable checks and the
		// termination gate has no executed-check ledger to judge on — the main's "done" faces only
		// the council's plausibility vote. Author one check for the objective (a single synthetic
		// step) so solo work still lands a verifiable contract. Coverage-gated + best-effort; council
		// or workflow presence is not required (the step gate runs independently).
		if !a.cfg.Workflow {
			solo := []planStep{{Title: prompt, Strategy: "solo"}}
			// Ground even a solo turn in the real repository — ESPECIALLY when solo is a FALLBACK from a
			// planner that couldn't emit a parseable plan (the weak model was already struggling, so it
			// most needs the real signatures/paths). The empty-repo skip makes this free on greenfield.
			a.exploreSpecMine(ctx, s, prompt, solo, depth)
			a.storeCoveredChecks(ctx, s, prompt, solo, nil)
		}
		return false, false // solo — the default, cheap path
	}

	// Plan-based spec mining: a read-only subagent explores the REAL repository against the plan and
	// injects the concrete existing signatures/paths/interfaces the steps must match — BEFORE the plan
	// audit authors its checks, so the check-author (and executor) ground on the real thing instead of
	// guessing. The prompt-analysis half already ran on the request alone at the front; this is the
	// plan-based half. Best-effort, top-level only; a no-op when disabled or the spawn yields nothing.
	a.exploreSpecMine(ctx, s, prompt, steps, depth)

	// Plan audit (D17): a multi-step procedure is reviewed by the council BEFORE it
	// runs. Suppressed in workflow mode (the deterministic engine owns staging) and
	// when no council is configured. minAudit is normally 2 (a lone step has nothing to
	// order), but soloAuditEnabled lowers it to 1 so a 1-step plan still gets the per-step
	// deliverable criteria/checks the completion gate needs — the cancel-async gap.
	minAudit := 2
	if soloAuditEnabled() {
		minAudit = 1
	}
	if len(steps) >= minAudit && a.cfg.Council != nil && !a.cfg.Workflow {
		steps = guardExpansion(a.runPlanAuditGate(ctx, s, spec, prompt, steps, depth, maxSteps), depth, a.cfg.MaxPlanDepth)
		if len(steps) == 0 {
			return false, false
		}
		// (a single remaining step is fine — nothing to fan out, but solo work follows)
	}

	// Route solo→delegate NOW, before the plan is shown or run — so the registered todos, the plan
	// event, and executeSteps all reflect the SAME strategy. Without this the user saw "[solo]" steps
	// that silently ran on a worker (the rewrite used to happen per-step inside executeSteps).
	steps = a.forceDelegateSteps(steps)

	a.registerPlanTodos(ctx, s.ID, steps)
	a.emitPhase(s.ID, "plan", planSummary(steps), strings.TrimSpace(plan.Reason))

	// Spec fidelity and checkpoint-first are NO LONGER injected as per-turn execution notes here:
	// both were redundant with plan-stage machinery and only bloated the context. Literal fidelity
	// lives in the planner contract's literalRule (below) and the curated brief's verbatim `literals`;
	// checkpoint-first lives in the planner contract's checkpointFirstRule (which orders an early
	// checkpoint STEP), the plan-audit's executable deliverable checks (the concrete checkpoints the
	// worker runs), and a standing rule in the executor's system prompt — so the discipline is kept in
	// the plan and the work rules rather than re-stated as a message every turn.

	// Async explorer preflight: a top-level plan with NO write step is pure investigation.
	// Dispatch its explorers to the BACKGROUND instead of blocking here, so the orchestrator
	// loop parks in its bg-wait and stays responsive to user interjections during the fan-out
	// (see runLoop's early park + needsOrchestratorTurn). Their findings arrive as messages
	// (injectSubagentResult) and the orchestrator synthesizes the review from them — no
	// injectPlannerFindings on this path. A mixed plan (has delegate/refine) keeps the
	// synchronous executeSteps below, so a write step still sees prior findings in its brief.
	// When force-delegate will route this plan's SOLO steps to workers (executeSteps, below), the
	// plan is no longer pure investigation even though hasWriteStep is false — so DON'T take the
	// read-only explorer shortcut, or those steps run solo on the main agent and the worker/curator
	// path never engages (a bench showed every all-solo plan bypassing it here).
	forcingWorkers := forceDelegateEnabled() && len(a.delegatableAgents()) > 0
	if asyncExplorersEnabled() && depth == 0 && !a.cfg.Workflow && !a.hasWriteStep(steps) && !forcingWorkers {
		if a.dispatchExplorerSteps(ctx, s, prompt, steps, depth) {
			return true, false // explorers dispatched; the loop parks, answers interjections, then synthesizes
		}
		return false, false // nothing to dispatch (all solo / empty groups) → solo path
	}

	findings, delegated := a.executeSteps(ctx, s, prompt, steps, depth)
	if strings.TrimSpace(findings) == "" {
		return false, false
	}
	a.injectPlannerFindings(ctx, s.ID, findings, delegated)
	return true, delegated
}

// hasWriteStep reports whether any step in the plan carries out WRITE work (delegate/refine) —
// i.e. it is dispatched through a writeStepRunner rather than the read-only explorer path. Used
// to gate the async-explorer fast path to pure-investigation plans only.
func (a *App) hasWriteStep(steps []planStep) bool {
	for _, st := range steps {
		if a.writeStepRunner(st.Strategy) != nil {
			return true
		}
	}
	return false
}

// dispatchExplorerSteps fans out a pure-read-only plan's explorer groups as BACKGROUND subagents
// (a.dispatch) rather than blocking on them. It walks steps in order — the same strategy handling
// as executeSteps, minus write steps (the caller guarantees none) — and returns true once at least
// one explorer was dispatched, so the caller can seed usedTools and the loop parks for the results.
// The per-turn explorer budget (maxPlanExplorers) is still honored. Scout's discover phase runs
// synchronously (a quick single explorer that yields the work-list); its per-item explorers are
// what get backgrounded.
func (a *App) dispatchExplorerSteps(ctx context.Context, s session.Session, goal string, steps []planStep, depth int) bool {
	budget := maxPlanExplorers
	fanGoal := ""
	if !stepContextDisabled() {
		fanGoal = goal // orient read-only explorers with the overall goal (mirrors executeSteps)
	}
	dispatched := 0
	for i, st := range steps {
		if budget <= 0 || ctx.Err() != nil {
			break
		}
		var groups []planGroup
		switch st.Strategy {
		case "parallel":
			groups = capGroups(st.Groups, &budget)
		case "scout":
			groups = a.scoutGroups(ctx, s, fanGoal, st, &budget, depth)
		default: // solo → main agent does it; nothing to dispatch
			continue
		}
		if len(groups) == 0 {
			continue // per-step degrade
		}
		a.advanceTo(ctx, s.ID, plannerActor, i) // moved on to step i: earlier steps ✓, step i running ◐
		for _, g := range groups {
			// dispatch is non-blocking: it bumps bgOutstanding, runs the explorer in a goroutine,
			// and injects the result as a message when done. Duplicate (agent,prompt) pairs are
			// deduped inside dispatch — explorer groups carry distinct focus/question, so they don't
			// collide. The ctx is the turn ctx, alive for the whole loop that follows; Timeout
			// bounds the whole spawn (restarts included) like the sync path's explorerTimeout —
			// without it a churning explorer holds the parked parent for its full restart budget.
			a.dispatch(ctx, s, depth, port.SpawnRequest{
				Agent: g.Agent, Prompt: explorerPrompt(fanGoal, g), Timeout: explorerTimeout,
			})
			dispatched++
		}
	}
	if dispatched == 0 {
		return false
	}
	// Mark this turn as awaiting explorer results: the loop parks pre-model (no findings-less
	// review) until they report. Scoped to this signal so ordinary background delegation still
	// interleaves the orchestrator's own work (TestOrchestratorInterleavesOwnWork).
	a.setAwaitExplorers(s.ID, true)
	a.injectAsyncExplorerNote(ctx, s.ID, dispatched)
	return true
}

// injectAsyncExplorerNote tells the orchestrator that N read-only explorers are running in the
// background and their findings will arrive as messages — the async counterpart to
// injectPlannerFindings' "trust your own explorers, synthesize from them" framing, plus the note
// that it may answer user messages while it waits.
func (a *App) injectAsyncExplorerNote(ctx context.Context, sid session.SessionID, n int) {
	text := fmt.Sprintf("# Investigation in progress — %d read-only explorer subagent(s) dispatched\n\n"+
		"You dispatched %d read-only explorer(s) to investigate this task. Their findings will arrive as "+
		"messages ([subagent … result]). Treat them as YOUR OWN explorers' results — your primary source — "+
		"and SYNTHESIZE the answer directly from them; do not re-investigate what they cover. A plan (todos) "+
		"is set for this task — CONTINUE and update those todos as you go; do not replace them wholesale. "+
		"Do NOT read/grep/investigate the codebase yourself while the explorers run — they OWN that "+
		"investigation and duplicating it wastes turns and races their work. If a user message arrives, "+
		"answer that aside briefly, then wait for the findings.", n, n)
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "planner"}, text)
}

// replanContext is what a RE-plan needs beyond the fresh critique: the plan the planner itself
// produced last round, and the convergence judge's ruling on the rewrite before this one. Neither
// is recoverable from the conversation — a plan lives inside a council event, and reconstruct
// turns only prompts and appended parts into messages, so a side call like runPlanner sees no
// trace of its own prior output. Without them the instruction header ("your previous plan") names
// something absent from the window and the re-planner rewrites from the critique alone: it cannot
// tell a step it deliberately dropped from one it forgot, and it re-submits the same rewrite a
// judge already ruled unresponsive. The zero value is a first plan — nothing to remember yet.
type replanContext struct {
	priorPlan string // rendered steps of the plan the critique is about
	judge     string // why the LAST rewrite was ruled not to engage the concern ("" on the first revision)
	// emptyReply, when set, marks this call as the retry of a re-plan that came back unusable, and
	// says how. A retry that re-sends the identical prompt asks the same question and mostly gets
	// the same answer back.
	emptyReply string
}

// runPlanner does a single tool-free LLM call on the planner's own provider and
// parses the procedure from the reply. revise is non-empty on a re-plan after a
// council plan-audit asked for changes, and rc then carries what that re-plan is
// revising (see replanContext). Returns a zero planResult on any error.
// anchor, when non-empty, is the exact task the plan must decompose — appended as a final
// instruction so it survives even when the conversation window (plannerWindow's byte budget)
// drops the original prompt. Used on a re-plan after route_interjection: a long turn's explorer
// results can push the original goal out of the window, leaving only the steer, so the adopted
// turnTask (original goal + the steer's constraint) is re-anchored here explicitly.
func (a *App) runPlanner(ctx context.Context, spec AgentSpec, s session.Session, prompt, revise string, rc replanContext, depth, maxSteps int, anchor string) planResult {
	repoBlock := "# Repository (top level)\n" + repoMap(s.Workdir)
	sys := spec.System + "\n\n" + repoBlock + "\n\n" + plannerContract + planEnvelope(depth, a.cfg.MaxPlanDepth, maxSteps)
	// Diverge only on RE-plans: an audit revision (revise != "") or a stuck-recovery
	// decompose (anchor != "" — those callers pass the task as the anchor). The three
	// post-mortems that motivated the clause were all about switching axes AFTER a
	// first approach stalled; on a FIRST plan the clause only inflated clear-approach
	// plans (heavier steps, extra audit rejections — observed on extract-elf), so the
	// initial plan keeps the baseline contract.
	if divergeEnabled() && (strings.TrimSpace(revise) != "" || strings.TrimSpace(anchor) != "") {
		sys += divergeClause
	}
	if specFidelityEnabled() {
		sys += literalRule
	}
	if checkpointFirstEnabled() {
		sys += checkpointFirstRule
	}
	if implicitAcceptEnabled() {
		sys += implicitAcceptRule
	}
	if names := a.delegatableAgents(); len(names) > 0 {
		sys += "\n\nDelegate executors available (use one as a delegate step's \"agent\"): " + strings.Join(names, ", ") +
			". PREFER delegating each substantial, self-contained chunk of the WORK — writing/building/running/fixing — " +
			"to an executor as a \"delegate\" step rather than doing it yourself as \"solo\": execution then runs in the " +
			"worker's own scoped context. Keep \"solo\" only for small glue and for reasoning/analysis steps."
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
		var b strings.Builder
		// The plan being revised comes FIRST, because the critique below is meaningless without it.
		// It is not in the window (see replanContext), so a re-planner without it treats the review
		// as a fresh request and rewrites from scratch: whatever the critique did not happen to
		// mention gets silently dropped, including work an earlier round of the same audit gained.
		if p := strings.TrimSpace(rc.priorPlan); p != "" {
			b.WriteString("# Your previous plan — the one the review below is about (it is NOT in the conversation above)\n" +
				clipSpec(p, 1500) + "\n\nRevise THIS plan. Everything the review does not object to stays as it is.\n\n")
		}
		b.WriteString("# Council review of your previous plan (address this and re-plan):\n" + revise)
		// A rewrite the judge already ruled unresponsive is the one rewrite worth naming: without
		// it the planner meets the same critique with the same non-answer, and the audit spends its
		// remaining rounds re-deciding a question it has already decided.
		if j := strings.TrimSpace(rc.judge); j != "" {
			b.WriteString("\n\n# Your LAST rewrite was judged NOT to engage this same concern\n" + clipSpec(j, 600) +
				"\nDo not repeat it. Change what the concern actually names — or, if you believe the concern is wrong, contest it.")
		}
		// Last, so it is the final thing read on a retry: this is the one instruction that is about
		// the REPLY rather than the plan.
		if e := strings.TrimSpace(rc.emptyReply); e != "" {
			b.WriteString("\n\n# This is a RETRY — your previous reply was unusable\n" + e)
		}
		// CONTEST channel: if the concern is genuinely NOT required by the task (or already satisfied),
		// the planner may reject it instead of complying — keep the plan and give a task-grounded
		// rebuttal in `contest` for the council to re-judge, rather than churn on a phantom demand.
		if planContestEnabled() {
			b.WriteString("\n\nIf you judge this concern is UNJUSTIFIED — the TASK as written does not require what it " +
				"demands, or your plan already satisfies it — do NOT distort the plan to comply. Instead KEEP your plan " +
				"and set the top-level \"contest\" field to a one-sentence rebuttal that cites the TASK's own words (e.g. " +
				"\"the task never asks for X; step N already produces Y\"). The council re-judges it: a real concern is " +
				"upheld, an over-demand is dropped. Only contest when you can ground it in the task — otherwise revise.")
		}
		msgs = append(msgs, session.Message{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: b.String()}}})
	}
	if len(msgs) == 0 { // defensive: never call with an empty conversation
		msgs = []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: prompt}}}}
	}
	if anc := strings.TrimSpace(anchor); anc != "" {
		msgs = append(msgs, session.Message{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText,
			Text: "# Task to plan now (decompose THIS exact task; it supersedes earlier framing):\n" + anc}}})
	}
	// Contract-first (D-contract): a council-reviewed acceptance contract was frozen before planning.
	// Give it to the planner as the target the plan must satisfy — every criterion/check should be
	// achieved by some step — so the plan is built around a reviewed contract, not an open reading.
	if contract := a.contractForPlanner(s.ID); contract != "" {
		msgs = append(msgs, session.Message{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText,
			Text: "# Acceptance contract to satisfy (council-reviewed — plan so that EVERY item below is achieved by some step):\n" + contract}}})
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
	// A planner generation is a side LLM call: it produces no PartDelta events, so
	// on a slow model the minutes it takes are indistinguishable from a hang in the
	// TUI and headless stream (observed: 90 min of re-plan rounds rendering as one
	// silent line). Emit transient progress at entry and on a parse failure so a
	// live plan phase is visibly alive and a silent-retry loop is visible AS a loop.
	tag := "plan"
	if strings.TrimSpace(revise) != "" {
		tag = "re-plan (council revision)"
	} else if strings.TrimSpace(anchor) != "" {
		tag = "re-plan (decompose)"
	}
	a.emitToolProgress(s.ID, plannerActor, "", "planner", tag+": generating…")
	text, err := a.drainText(ctx, spec, req) // stall-watchdog drain: a hung re-plan generate aborts, not wall-clock
	if err != nil {
		return planResult{}
	}
	res, kind := readPlan(text)
	if kind.salvaged() {
		a.emitToolProgress(s.ID, plannerActor, "", "planner",
			fmt.Sprintf("%s: recovered %d step(s) from an unparseable plan via salvage (%d chars) :: %s",
				tag, len(res.Steps), len(text), planParseExcerpt(text)))
	}
	// A PARTIAL read is not a rescue, it is a silent edit: the steps that survived look like a complete
	// plan, and every downstream stage — the audit, the checks, the workers — judges only what survived.
	// The steps the defect swallowed are unrecoverable from this reply, so ask ONCE for the whole plan;
	// the salvaged one is kept unless the retry comes back strictly better (read whole, or carrying more
	// steps), which keeps a worse second answer from displacing a usable first one.
	if kind == planPartial && len(res.Steps) > 0 {
		a.emitToolProgress(s.ID, plannerActor, "", "planner",
			fmt.Sprintf("%s: the salvage read only PART of the reply — %d step(s) survived, any step inside the "+
				"damaged span is lost; re-asking for the whole plan", tag, len(res.Steps)))
		a.recordParseFailure(ctx, s.ID, "planner", tag+"-partial", text, planProbe) // the raw reply is the only record of what was dropped
		retry := req
		retry.System = sys + "\n\n" + plannerRetryReminder(text, true)
		if text2, err2 := a.drainText(ctx, spec, retry); err2 == nil {
			r2, k2 := readPlan(text2)
			if len(r2.Steps) > 0 && (k2 != planPartial || len(r2.Steps) > len(res.Steps)) {
				a.emitToolProgress(s.ID, plannerActor, "", "planner",
					fmt.Sprintf("%s: re-ask returned %d step(s) (%s) — using it instead of the %d salvaged",
						tag, len(r2.Steps), kindLabel(k2), len(res.Steps)))
				return r2
			}
			a.emitToolProgress(s.ID, plannerActor, "", "planner",
				fmt.Sprintf("%s: re-ask did not improve on the salvage (%d step(s), %s) — keeping the salvaged plan :: %s",
					tag, len(r2.Steps), kindLabel(k2), planParseExcerpt(text2)))
		}
	}
	if len(res.Steps) == 0 {
		// Weak models often bury the JSON under pages of reasoning, or ramble until the output
		// budget cuts the object off mid-string — both leave no balanced plan object to parse
		// (fix-ocaml-gc died this way: a 4247-char reply, then a 1841-char re-plan, both
		// unparseable → no plan → the solo fallback flailed into the loop guard). Give ONE focused
		// re-ask that forbids all prose, so the bare object is emitted (and fits the budget).
		r2, ok2 := reask[planResult]{
			pass:  "planner",
			label: tag,
			actor: plannerActor,
			ask: func(system string) (planResult, string, bool) {
				retry := req
				retry.System = system
				text2, err2 := a.drainText(ctx, spec, retry) // same stall-watchdog drain as the first ask
				if err2 != nil {
					return planResult{}, "", false
				}
				r, salvaged := parsePlanOrSalvage(text2)
				if salvaged && len(r.Steps) > 0 {
					// A recovered plan is usable but NOT the same event as one that simply parsed, and
					// the difference is the model's, not the run's — say which one happened.
					a.emitToolProgress(s.ID, plannerActor, "", "planner",
						fmt.Sprintf("%s: the JSON-only re-ask recovered %d step(s) via salvage", tag, len(r.Steps)))
				}
				return r, text2, len(r.Steps) > 0
			},
			defect: func(p planResult, _ bool, raw string) string {
				if len(p.Steps) > 0 {
					return ""
				}
				return fmt.Sprintf("no parseable plan (%d chars)", len(raw))
			},
			reminder: func(raw string, _ bool) string { return sys + "\n\n" + plannerRetryReminder(raw, false) },
			probe:    planProbe,
			fallback: "still no plan, and this phase will proceed without one",
		}.run(ctx, a, s.ID, res, text, false)
		if ok2 {
			return r2
		}
	}
	return res
}

// planJSONOnlyReminder is appended to the planner system prompt on a retry after an unparseable
// reply: strip ALL prose so the model emits the bare JSON object (which also keeps it inside a
// tight output budget that reasoning would otherwise overflow).
const planJSONOnlyReminder = "CRITICAL: your previous reply could not be parsed as JSON. Reply with " +
	"ONLY the JSON object specified above — no reasoning, no explanation, no markdown fence, nothing " +
	"before the opening `{` or after the closing `}`. Begin your reply with `{` and end it with `}`."

// plannerRetryReminder names the defect that ACTUALLY occurred. The single prose-stripping reminder
// above is right for a reply that buried the object in reasoning and wrong for every other shape: a
// model that already sent a bare object and merely mis-nested one container is told to remove prose
// it never wrote, so it re-sends the same malformation. jsonx.Diagnose already computes the offset
// and the ⟪HERE⟫ window for the log — give it to the only party that can act on it. `partial` marks
// the case where steps WERE recovered: the ask is not "reply in JSON" (it did) but "send the plan
// whole", and it must say what was lost or the model has no reason to change anything.
func plannerRetryReminder(text string, partial bool) string {
	d := jsonx.Diagnose(text)
	var b strings.Builder
	b.WriteString("CRITICAL: your previous reply could not be read as a whole plan. ")
	if partial {
		b.WriteString("It DID contain JSON, and some step objects were recovered from it — but the object as a " +
			"whole is malformed, so any step inside the damaged region was DROPPED and the plan now being " +
			"considered is missing work you wrote:\n")
	} else if strings.HasPrefix(d, "syntax error") {
		b.WriteString("It DID contain a JSON object, so the problem is not prose around it — the JSON itself is " +
			"malformed:\n")
	} else {
		return planJSONOnlyReminder // prose-wrapped, truncated, or schema-shaped: the original advice is the right one
	}
	b.WriteString(d)
	b.WriteString("\nSend the COMPLETE plan again as ONE well-formed JSON object with every step present. " +
		"Every `[` must be closed by `]` before the next key begins, every `{` by exactly one `}` (a stray " +
		"closing brace ends the object early and silently truncates the plan), and every string by its " +
		"closing quote. No reasoning, no markdown fence — begin with `{` and end with `}`.")
	return b.String()
}

// kindLabel renders a planReadKind for the run log, so a re-ask that was taken (or rejected) says why.
func kindLabel(k planReadKind) string {
	switch k {
	case planWhole:
		return "read whole"
	case planRepaired:
		return "repaired, steps intact"
	default:
		return "partial salvage"
	}
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

// parsePlan extracts the plan JSON object from a model reply and unmarshals it. Models
// wrap the object in prose/fences, and weak local models often precede it with reasoning
// that contains a STRAY '{' — a code fragment ("`{last_free_block}`"), a set literal, an
// example. Taking only the FIRST balanced object then grabs that stray and fails to parse,
// silently degrading a real multi-step plan to nothing (observed in a stuck-decompose
// re-plan: a 1717-char reply "yielded no parseable plan"). So scan EVERY top-level balanced
// object and take the first that unmarshals into a plan with at least one step. If none has
// steps, fall back to the first that unmarshals at all (preserves the old shape for a
// legitimately-empty plan); else empty.
func parsePlan(text string) planResult { r, _ := parsePlanOrSalvage(text); return r }

// planReadKind says HOW a plan was read, because "salvaged" covers two cases the caller must treat
// differently. A repaired reply still yielded the plan object itself, so its steps array is whole and
// nothing was dropped. A step-salvaged reply did NOT: the outer object never parsed, so the steps are
// whatever balanced step objects happened to survive OUTSIDE the damaged span — and any step the
// defect swallowed is simply gone, with the recovered ones looking exactly like a complete plan.
// (Observed: a stray `},}` after the first step made the outer object balance early, so that first
// step lived inside the unparseable span and the run proceeded on the remaining two as if the model
// had planned two. The plan audit caught it, but only by chance of judging the content.)
type planReadKind int

const (
	planWhole    planReadKind = iota // parsed as written
	planRepaired                     // needed a repair, but the plan object itself parsed: steps intact
	planPartial                      // steps scavenged from a reply whose plan object never parsed: LOSSY
)

func (k planReadKind) salvaged() bool { return k != planWhole }

// parsePlanOrSalvage is parsePlan plus a `salvaged` flag: true when the plan's steps were recovered
// from an unparseable (truncated/malformed) reply rather than a clean parse, so the caller can log
// that the salvage path did the work — otherwise a rescued truncation is indistinguishable from a
// clean first-try parse in the run.
func parsePlanOrSalvage(text string) (planResult, bool) {
	r, kind := readPlan(text)
	return r, kind.salvaged()
}

// readPlan is parsePlanOrSalvage reporting WHICH salvage path ran (see planReadKind).
func readPlan(text string) (planResult, planReadKind) {
	var firstValid *planResult
	// Which spans the reply yielded WITHOUT repair: a plan recovered from a damaged reply (truncated,
	// or carrying a stray token that cost the span) is still reported as salvaged, because the run
	// treats a rescued partial plan differently from one the model wrote whole — and a repair that
	// silently erased that distinction would be worse than the truncation it fixed.
	intact := map[string]bool{}
	for _, js := range jsonx.BalancedObjects(text) {
		intact[js] = true
	}
	for _, js := range balancedObjects(text) {
		p, ok := unmarshalPlanLenient(js)
		if !ok {
			continue // not JSON, or not the plan shape — try the next object
		}
		if len(p.Steps) > 0 {
			if intact[js] {
				return p, planWhole // a real plan wins immediately
			}
			return p, planRepaired
		}
		if firstValid == nil {
			pp := p
			firstValid = &pp
		}
	}
	// Salvage BEFORE returning firstValid: a plan object TRUNCATED by the output budget never closes its
	// outer {}, so no planResult with steps parses — but the step objects emitted before the cutoff are
	// still balanced and recoverable. It must run before firstValid because a lone step object ALSO
	// unmarshals as a stepless planResult (JSON tolerates the missing "steps" field), so firstValid can
	// be a spurious empty plan captured from the first recovered step. Recovered steps win.
	if steps := salvageSteps(text); len(steps) > 0 {
		return planResult{Steps: steps}, planPartial
	}
	if firstValid != nil {
		return *firstValid, planWhole
	}
	return planResult{}, planWhole
}

// planStrategies is the set of recognized step strategies — used to tell a real plan step object from
// a nested group or a stray brace when salvaging steps from an unparseable reply.
var planStrategies = map[string]bool{"solo": true, "parallel": true, "scout": true, "delegate": true, "refine": true}

// salvageSteps recovers plan STEP objects directly from a reply whose overall plan object did not
// parse — the truncation case, where the outer {} was cut off but the earlier step objects are
// complete and balanced (balancedObjects skips the unclosed outer brace and returns them at depth 0).
// It accepts a balanced object only when it looks like a real step — a non-empty title AND a
// recognized strategy — so a nested group, a deliverable object, or a stray brace is never mistaken
// for a step. Steps are returned in reply order.
func salvageSteps(text string) []planStep {
	var steps []planStep
	for _, js := range balancedObjects(text) {
		st, ok := unmarshalStepLenient(js)
		if !ok {
			continue
		}
		if strings.TrimSpace(st.Title) == "" || !planStrategies[strings.ToLower(strings.TrimSpace(st.Strategy))] {
			continue
		}
		steps = append(steps, st)
	}
	return steps
}

// unmarshalStepLenient parses one salvaged step object with the same weak-model repairs the plan
// object gets (jsonRepairCandidates). A step carries the multi-line "task" string, so a raw control
// character inside it is the single likeliest defect in a reply that ALSO truncated — the case
// salvage exists for. Parsing it strictly here silently discarded every complete step before the
// cutoff and sent an otherwise-recoverable revision to the JSON-only retry.
func unmarshalStepLenient(js string) (planStep, bool) {
	var st planStep
	if unmarshalLenient(js, &st) {
		return st, true
	}
	return planStep{}, false
}

// planParseExcerpt renders an unparseable planner reply for the run log: a single-line head+tail
// AND the named reason it could not be read, so the failure mode (truncation, prose, a syntax defect
// in the MIDDLE that the head+tail hides, or a schema mismatch) is readable from the record instead
// of only its length.
func planParseExcerpt(text string) string { return jsonx.Report(text) }

// planProbe reads a candidate JSON object as a plan. It is the planner's contribution to the shared
// parse-failure classifier: unmarshalling into the plan TYPE is what separates "valid JSON" from
// "valid JSON of the wrong shape", which a probe into a bare any cannot see.
func planProbe(b []byte) error { var p planResult; return json.Unmarshal(b, &p) }

// unmarshalPlanLenient parses one candidate object as a plan, tolerating a trailing comma before a
// closing } or ] — a very common weak-model JSON error that json.Unmarshal rejects outright, and one
// that otherwise dumps an entire valid-but-for-one-comma plan into the solo fallback.
func unmarshalPlanLenient(js string) (planResult, bool) {
	var p planResult
	if unmarshalLenient(js, &p) {
		return p, true
	}
	return planResult{}, false
}

// unmarshalLenient parses js into v, retrying with the shared weak-model repairs before failing.
// Every reader of model-produced JSON needs it for the same reason: the payloads carry multi-line
// prose (a reason, a task, a criterion) or shell commands, so an unescaped control character is the
// normal shape of the data rather than an edge case — and rejecting the document over one discards
// content that was otherwise complete.
func unmarshalLenient(js string, v any) bool { return jsonx.Unmarshal(js, v) }

// jsonRepairCandidates returns js followed by the weak-model repair variants a failed unmarshal
// should be retried with: a trailing comma before }/] , a RAW control character (literal
// newline/tab) inside a string value, and both together. Each is an error json.Unmarshal rejects
// outright but that a weak model routinely emits — a multi-line "reason" or "task" string is the
// common source of the control char. Candidates are de-duplicated and ordered cheapest-first, so a
// clean object still parses on the first try.
//
// It is shared by every lenient JSON reader (the plan object AND the salvaged step objects): a
// repair the clean path applies but the salvage path does not is worse than useless, since the
// truncation that forces salvage is produced by exactly the rambling that emits these defects.
func jsonRepairCandidates(js string) []string { return jsonx.RepairCandidates(js) }

// escapeControlCharsInStrings rewrites raw control characters (< 0x20) that appear INSIDE a JSON
// string literal into their valid JSON escape (a literal newline/tab a weak model puts inside a
// "reason"/"task" value becomes \n/\t), which json.Unmarshal otherwise rejects with "invalid
// character ... in string literal" and stripTrailingCommas does not touch. Control characters
// OUTSIDE strings — the whitespace between tokens — are left exactly as they are, so only the illegal
// in-string ones are repaired. Respects escapes, so an already-escaped sequence is never doubled.
func escapeControlCharsInStrings(s string) string { return jsonx.EscapeControlCharsInStrings(s) }

// stripTrailingCommas removes a comma that immediately precedes a closing } or ] (ignoring
// intervening whitespace). It respects string literals — a comma inside a quoted value is untouched —
// so it only repairs the structural trailing comma JSON forbids but weak models routinely emit.
func stripTrailingCommas(s string) string { return jsonx.StripTrailingCommas(s) }

// balancedObjects returns every TOP-LEVEL balanced {...} object in s, in order, respecting
// strings and escapes (braces inside string values don't confuse it). Nested objects (a
// plan's step objects) stay inside their parent — only depth-0 spans are returned — so the
// caller can try each candidate independently and skip a stray brace that precedes the real
// object.
func balancedObjects(s string) []string { return jsonx.Objects(s) }

// balancedArrays is balancedObjects for [...] arrays — every TOP-LEVEL balanced array in s, in
// order, respecting strings and escapes. A JSON-array reply (e.g. a check-audit's list) that is
// wrapped in prose or trailed by reasoning containing a stray ] is recovered by trying each
// candidate, instead of a naive first-[/last-] span that mis-captures on any bracket outside the
// real array.
func balancedArrays(s string) []string { return jsonx.Arrays(s) }

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
		case "solo", "parallel", "scout", "delegate", "refine":
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
		case "refine":
			if strings.TrimSpace(st.Task) == "" {
				continue // a refine with no sub-goal is meaningless
			}
			if refineDisabled() {
				st.Strategy = "solo" // bench A/B OFF arm: reproduce the pre-refine baseline (sub-goal flattens inline)
			} else {
				st.Agent = strings.TrimSpace(st.Agent) // optional: refine runs in-session (context clone), not a separate executor
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

// guardExpansion enforces the recursion policy on a freshly sanitized procedure, keyed on
// the depth it will execute at. Two always-on guardrails (they only ever downgrade refine→solo,
// never the reverse), a deterministic backstop to the same rules planEnvelope states to the
// planner in prose:
//
//	Depth cap — a refine step at depth d is expanded by a child that re-plans at d+1, which
//	only runs while d+1 < MaxPlanDepth (planEligible). AT the cap (d+1 >= MaxPlanDepth) a refine
//	step could never be expanded, so it would dead-end; downgrade every refine to solo (the work
//	is done inline here) rather than emit an abstract step that goes nowhere.
//
//	No pure re-deferral — below the cap, an EXPANSION (depth >= 1: this plan is itself the body
//	of a refine step) may nest further refine phases, but only alongside real work — it must hold
//	at least one concrete WORK step (solo or delegate). A depth>=1 plan that is ALL refine just
//	re-defers without progress, so its refine steps are downgraded to solo.
//
// Depth 0 (the top-level plan) is exempt from the second rule: opening a hard task with a few
// abstract phases and no flat step is the intended refine use (see plannerContract's example).
func guardExpansion(steps []planStep, depth, maxPlanDepth int) []planStep {
	hasRefine, hasWork := false, false
	for _, st := range steps {
		switch st.Strategy {
		case "refine":
			hasRefine = true
		case "solo", "delegate":
			hasWork = true
		}
	}
	if !hasRefine {
		return steps
	}
	atCap := depth+1 >= maxPlanDepth
	if !atCap && !(depth >= 1 && !hasWork) {
		return steps // below the cap and either top-level or has real work → refine is fine
	}
	for i := range steps {
		if steps[i].Strategy == "refine" {
			steps[i].Strategy = "solo"
		}
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

// stepSummaries renders each step as a compact "[strategy] title" line — the structured
// before/after material for a PlanRevised diff (a sibling of renderSteps' numbered prose).
func stepSummaries(steps []planStep) []string {
	out := make([]string, len(steps))
	for i, st := range steps {
		out[i] = fmt.Sprintf("[%s] %s", st.Strategy, st.Title)
	}
	return out
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
		// The concern is about a plan whose steps are mostly executed by WORKERS, and this
		// system message reaches only the session it is appended to. Keep it where the workers'
		// briefs can pick it up (concernBrief) — otherwise the council spends three rounds
		// establishing that a step must capture its build output, and the agent that actually
		// runs that step never hears it. Only the UNRESOLVED kind is carried: approved advice is
		// advisory by construction, and forwarding all of it would bury each worker's own part.
		a.mu.Lock()
		a.stateLocked(sid).planConcern = strings.TrimSpace(advice)
		a.mu.Unlock()
	}
	text := "# Plan review — notes for execution\n\n" + advice + "\n\n---\n" + tail
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "council"}, text)
}

// cachedConcern returns the plan council's unresolved critical concern for this turn, or "".
func (a *App) cachedConcern(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.planConcern
	}
	return ""
}

// injectSteerConstraint folds a mid-turn "append" steer into the RUNNING turn as a
// constraint, without re-planning. The approved plan is frozen for the turn's lifetime;
// the steer adjusts HOW the in-progress work is carried out, not WHAT is planned. The
// steer is still enforced at completion because the loop keeps turnTask = original+steer,
// so the termination council judges against both. This is the append counterpart to a
// redirect (which does re-plan, because the goal itself changed).
func (a *App) injectSteerConstraint(ctx context.Context, sid session.SessionID, steer string) {
	text := "# Mid-task steer (from the user)\n\n" + steer + "\n\n---\n" +
		"Apply this as a constraint on the work already in progress. KEEP the current plan and " +
		"todos — do NOT restart, re-plan, or re-decompose. Adjust only HOW you carry out the " +
		"remaining steps so this constraint is satisfied, and make sure it holds before you finish."
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "steer"}, text)
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
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "planner"}, text)
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
