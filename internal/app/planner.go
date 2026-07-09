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
// taskOverride, when non-empty, is the task the plan should decompose instead of the
// session's last user prompt — used when regrounding after a route_interjection so the
// re-plan follows the ADOPTED task (append folds the original goal + the steer's
// constraint) rather than the bare steer text, which alone loses the original intent.
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
	a.setStage(s.ID, stagePlan) // tag pre-flight planning events as the plan stage (D15)

	plan := a.runPlanner(ctx, spec, s, prompt, "", depth, maxSteps, strings.TrimSpace(taskOverride))
	a.storeStepEstimate(s.ID, plan.EstimatedSteps) // advisory pacing reference, solo or not
	steps := guardExpansion(sanitizeSteps(plan), depth, a.cfg.MaxPlanDepth)
	if len(steps) == 0 {
		a.emitPhase(s.ID, "plan", "solo", strings.TrimSpace(plan.Reason)) // ran, judged single-area
		return false, false                                               // solo — the default, cheap path
	}

	// Plan audit (D17): a multi-step procedure is reviewed by the council BEFORE it
	// runs. Suppressed in workflow mode (the deterministic engine owns staging) and
	// when no council is configured.
	if len(steps) >= 2 && a.cfg.Council != nil && !a.cfg.Workflow {
		steps = guardExpansion(a.runPlanAuditGate(ctx, s, spec, prompt, steps, depth, maxSteps), depth, a.cfg.MaxPlanDepth)
		if len(steps) == 0 {
			return false, false
		}
		// (a single remaining step is fine — nothing to fan out, but solo work follows)
	}

	a.registerPlanTodos(ctx, s.ID, steps)
	a.emitPhase(s.ID, "plan", planSummary(steps), strings.TrimSpace(plan.Reason))

	// Spec fidelity (Part B): a plan now governs execution, so the todos are a SUMMARY of the
	// request. Re-anchor the main agent on the original wording for literal identifiers before any
	// step runs — this fires ahead of executeSteps, so refine clones and the findings-synthesis
	// path inherit it via the parent context. All-solo plans (no findings) rely on this too.
	if specFidelityEnabled() {
		_ = a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "planner"}, specFidelityNote)
	}
	if checkpointFirstEnabled() {
		_ = a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "planner"}, checkpointFirstNote)
	}

	// Async explorer preflight: a top-level plan with NO write step is pure investigation.
	// Dispatch its explorers to the BACKGROUND instead of blocking here, so the orchestrator
	// loop parks in its bg-wait and stays responsive to user interjections during the fan-out
	// (see runLoop's early park + needsOrchestratorTurn). Their findings arrive as messages
	// (injectSubagentResult) and the orchestrator synthesizes the review from them — no
	// injectPlannerFindings on this path. A mixed plan (has delegate/refine) keeps the
	// synchronous executeSteps below, so a write step still sees prior findings in its brief.
	if asyncExplorersEnabled() && depth == 0 && !a.cfg.Workflow && !a.hasWriteStep(steps) {
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
			groups = a.scoutGroups(ctx, s, st, &budget, depth)
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
			// collide. The ctx is the turn ctx, alive for the whole loop that follows.
			a.dispatch(ctx, s, depth, port.SpawnRequest{Agent: g.Agent, Prompt: explorerPrompt(fanGoal, g)})
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

// runPlanner does a single tool-free LLM call on the planner's own provider and
// parses the procedure from the reply. revise is non-empty on a re-plan after a
// council plan-audit asked for changes. Returns a zero planResult on any error.
// anchor, when non-empty, is the exact task the plan must decompose — appended as a final
// instruction so it survives even when the conversation window (plannerWindow's byte budget)
// drops the original prompt. Used on a re-plan after route_interjection: a long turn's explorer
// results can push the original goal out of the window, leaving only the steer, so the adopted
// turnTask (original goal + the steer's constraint) is re-anchored here explicitly.
func (a *App) runPlanner(ctx context.Context, spec AgentSpec, s session.Session, prompt, revise string, depth, maxSteps int, anchor string) planResult {
	sys := spec.System + "\n\n# Repository (top level)\n" + repoMap(s.Workdir) + "\n\n" + plannerContract + planEnvelope(depth, a.cfg.MaxPlanDepth, maxSteps)
	if specFidelityEnabled() {
		sys += literalRule
	}
	if checkpointFirstEnabled() {
		sys += checkpointFirstRule
	}
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
	if anc := strings.TrimSpace(anchor); anc != "" {
		msgs = append(msgs, session.Message{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText,
			Text: "# Task to plan now (decompose THIS exact task; it supersedes earlier framing):\n" + anc}}})
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
	"worth handing off whole. If no executor agent is available (see below), don't use delegate.\n" +
	"- \"refine\": a large sub-GOAL that is NOT independent — it depends on or builds on earlier steps, or is too big to " +
	"state as one concrete action yet. State it at a HIGH LEVEL and give \"task\" (what the sub-goal must achieve). It is " +
	"expanded into concrete sub-steps AT EXECUTION TIME, WITH the current context carried forward (unlike delegate, which " +
	"hands an independent chunk to a context-free sub-agent). Use refine to break a HARD problem into a few abstract phases " +
	"you will each work out in detail as you reach them; a small or already-concrete action stays \"solo\". An abstract " +
	"refine step is fine — you are NOT required to spell out its internal actions here. " +
	"When a task is genuinely HARD and its parts are SEQUENTIALLY DEPENDENT — each stage needs the result of the one " +
	"before (a storage layer, THEN an index built ON that storage, THEN operations built ON that index) — PREFER " +
	"opening the plan with a few \"refine\" phases over one long flat list of \"solo\" steps; you will expand each phase in " +
	"context when you reach it. Keep flat \"solo\" lists for tasks whose steps are short and mostly independent.\n\n" +
	"Explorers are READ-ONLY (agent ∈ explore|locator|analyst); never use them to write. " +
	"Also give \"estimated_steps\": your honest guess at the TOTAL number of tool calls the whole task needs " +
	"(a one-file tweak ~5, a feature with tests ~30, a big build/debug ~100). It is pacing guidance only — never a limit.\n" +
	"Reply with ONLY a JSON object, no prose:\n" +
	`{"reason":"one sentence","estimated_steps":12,"steps":[{"title":"...","strategy":"solo|parallel|scout|delegate|refine","groups":[{"agent":"explore","focus":"...","question":"..."}],"agent":"explore","discover":"...","each":"...","task":"..."}]}` +
	"\n\nExample — a HARD, sequentially-dependent task (\"build a persistent key-value store backed by a " +
	"write-ahead log\") is opened as a few high-level \"refine\" PHASES, each worked out in context when reached, " +
	"NOT flattened into a long list of \"solo\" steps:\n" +
	`{"reason":"each layer builds on the one below, so open with phases and expand each in context","estimated_steps":40,"steps":[{"title":"On-disk write-ahead log","strategy":"refine","task":"design and implement the append-only log file format and an append operation"},{"title":"In-memory index from the log","strategy":"refine","task":"replay the log on startup to build a key to offset index"},{"title":"KV operations","strategy":"refine","task":"implement get/put/delete over the index and the log, keeping them consistent"},{"title":"Log compaction","strategy":"refine","task":"add a pass that rewrites the log to drop superseded records"}]}`

// literalRule is appended to the planner contract when specFidelityEnabled(): it forbids
// paraphrasing a literal contract, so the exact identifiers a grader checks survive into the
// step title/task (and from there into every downstream executor). See specFidelityEnabled.
const literalRule = "\n\nPRESERVE LITERALS: when the request specifies EXACT identifiers — a field/message/function " +
	"name, an output format, a numeric threshold, a path, or a literal string — reproduce it VERBATIM in the step " +
	"\"title\"/\"task\". Never paraphrase a literal contract (keep a field named `value` as `value`, not \"the value\"; " +
	"keep `YYYY-MM-DD` verbatim). The plan is a summary of the request, but its literals are NOT summaries."

// specFidelityNote is injected into the MAIN session once a plan governs execution (Part B): the
// todos summarize the request, so the executor is told to defer to the ORIGINAL wording for any
// exact identifier rather than normalizing it. Fires before executeSteps, so refine clones and the
// findings-synthesis path inherit it via the parent context. See specFidelityEnabled.
const specFidelityNote = "# Execution note — spec fidelity\n" +
	"The plan (todos) is a SUMMARY of this request. For any exact identifier — a name, field, " +
	"function/message, output format, threshold, or literal string — follow the ORIGINAL request's " +
	"wording VERBATIM, not the summary. Do NOT normalize or rename it (if the request names a field " +
	"`value`, keep it `value`, do not shorten it to `val`)."

// checkpointFirstNote is injected into the MAIN session before execution when
// checkpointFirstEnabled(): a test-first ordering directive. It fires on the same seam
// as specFidelityNote, so it reaches the solo, planned, and refine paths alike. See
// checkpointFirstEnabled.
const checkpointFirstNote = "# Execution note — checkpoint first\n" +
	"If this task states HOW your output will be checked or applied — a code snippet, a command, a " +
	"function call, or an input/output contract — then BEFORE implementing the solution, FIRST write a " +
	"small runnable checkpoint in the working directory that reproduces exactly that check (like writing " +
	"the test before the code). Build its inputs from the spec itself, INCLUDING any counter-example the " +
	"task names, and encode the expected result. Then implement until the checkpoint passes, and only " +
	"report done once you have RUN it and seen it pass. Do not invent an oracle the spec does not give — " +
	"reproduce the stated procedure. If a runnable checkpoint genuinely cannot be built cheaply (the " +
	"conditions are purely prose), proceed as usual."

// checkpointFirstRule is appended to the planner contract when checkpointFirstEnabled():
// it makes a multi-step plan ORDER the checkpoint early (a sequencing concern, not a new
// verification owner), so later steps implement against an artifact that already exists.
const checkpointFirstRule = "\n\nCHECKPOINT FIRST: if the request states HOW completion is checked or the output " +
	"applied (a snippet, command, function call, or I/O contract), make an EARLY step build a small runnable " +
	"checkpoint reproducing that check (inputs synthesized from the spec, including any named counter-example); " +
	"later steps implement until it passes. Only add this when the check is actually executable — do not pad a " +
	"prose-only task with it."

// planEnvelope gives the planner the two facts it otherwise plans blind to: the step
// BUDGET it is planning within, and its DEPTH relative to the recursion cap. Both let it
// right-size the procedure — a plan produced at the cap, or with little budget, should be
// small and concrete. The cap line is also a soft mirror of guardExpansion's hard rule:
// at the cap a "refine" step could never be expanded (planEligible needs depth < MaxPlanDepth,
// so a refine at depth d expands only while d+1 < MaxPlanDepth), so it must not be emitted.
func planEnvelope(depth, maxPlanDepth, maxSteps int) string {
	var b strings.Builder
	b.WriteString("\n\nBudget & depth (right-size the plan to these):\n")
	scope := "the whole task"
	if depth > 0 {
		scope = "this sub-task"
	}
	fmt.Fprintf(&b, "- About %d tool calls are available for %s — pacing guidance, not a hard limit.\n", maxSteps, scope)
	fmt.Fprintf(&b, "- Planning depth %d of max %d.\n", depth, maxPlanDepth)
	if depth+1 >= maxPlanDepth {
		b.WriteString("- You are AT the depth cap: every step MUST be concrete (solo/parallel/scout/delegate). " +
			"Do NOT use \"refine\" — an abstract step here can never be expanded, so it would dead-end; work it out inline instead.\n")
	} else {
		b.WriteString("- Below the cap: \"refine\" phases are allowed and are expanded in context when reached. " +
			"If this plan is itself an expansion of a refine step and uses \"refine\", it MUST also contain at least one " +
			"concrete work step — never a plan that only re-defers the work.\n")
	}
	return b.String()
}

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
		case "solo", "parallel", "scout", "delegate", "refine":
			return strings.TrimSpace(rest)
		}
	}
	return title
}

// sanitizeSteps enforces guardrails: valid strategies, read-only explorers, a
// usable shape per strategy, and a capped step count. A "solo" step is kept (it
// structures the procedure / todos) even though it dispatches nothing.
// refineDisabled is a bench A/B knob (mirrors MAGI_MAX_PLAN_DEPTH): MAGI_REFINE=0 downgrades
// refine steps to solo, reproducing the pre-refine baseline (every sub-goal flattened inline)
// so a paired refine-ON/OFF comparison can run on the same task. Default on.
func refineDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_REFINE"))) {
	case "0", "off", "false", "no":
		return true
	}
	return false
}

// stepContextDisabled is a bench A/B knob (mirrors MAGI_REFINE): MAGI_STEP_CONTEXT=0 turns
// OFF the compact context brief injected into delegate hand-offs and read-only fan-out — the
// children then run fully context-free (the pre-brief baseline), so a paired ON/OFF run can
// measure whether the brief helps. Default on. It gates ONLY the brief; it never copies the
// parent conversation (that stays refine's job) — so even ON, delegate/fan-out get an
// overall-goal line plus sibling boundaries/outputs, not the parent's reasoning history.
func stepContextDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_STEP_CONTEXT"))) {
	case "0", "off", "false", "no":
		return true
	}
	return false
}

// adaptDisabled turns OFF the REACTIVE (as-needed) failure re-decomposition — the ADaPT
// mechanism where a step that fails is retried by decomposing it further. MAGI_ADAPT=0 leaves
// only PLANNED decomposition (the up-front hierarchical plan) plus the stall safety net
// (redecomposeStuck): a failed delegate/refine node backtracks after one shot instead of
// spawning informed retries / re-decomposition. This is the recursion-policy A/B knob — with it
// off, magi is closer to HTN-style hierarchical planning with just-in-time sub-planning than to
// ADaPT proper. Default on (=current reactive behavior) so the flag flips the arm, not the base.
func adaptDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_ADAPT"))) {
	case "0", "off", "false", "no":
		return true
	}
	return false
}

// specFidelityEnabled keeps a plan faithful to the request's LITERAL contract — exact field/
// message/function names, output formats, thresholds, literal strings. Deep planning paraphrases
// the instruction, and the executor then normalizes a literal (kv-store-grpc: the request's
// `value` field became `val`, failing a grader that checks verbatim); a shallow/solo run reads the
// raw instruction directly and keeps the literal. This flag turns on three defenses (a planner
// "preserve literals" rule, a plan-time spec-fidelity note, and a verbatim SPEC anchor for the
// context-free delegate child). Default ON; MAGI_SPEC_FIDELITY=0 restores the paraphrase-only
// baseline (the A/B knob). Mirrors stepContextDisabled/adaptDisabled.
func specFidelityEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_SPEC_FIDELITY"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// checkpointFirstEnabled turns on test-first ordering: when a task states HOW its
// completion is checked (a snippet, command, function call, or I/O contract), the
// agent is told to FIRST materialize that as a runnable checkpoint in the workdir —
// synthesizing inputs from the spec, including any counter-example it names — and
// then implement until the checkpoint passes, rather than reasoning about a
// verifiable artifact symbolically and never running it (regex-log: a regex rewritten
// 7× with re.findall never once executed). A behavioral nudge on top of the existing
// end-of-turn unverifiedDeliverable backstop, not a replacement. Default OFF (opt-in,
// unvalidated): MAGI_CHECKPOINT_FIRST=1 enables it for an A/B arm. Mirrors
// specFidelityEnabled's env shape.
func checkpointFirstEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_CHECKPOINT_FIRST"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// asyncExplorersEnabled routes a top-level, read-only-only plan's explorer fan-out through the
// BACKGROUND dispatch path (a.dispatch) instead of the synchronous runExplorers, so the
// orchestrator loop parks in its bg-wait — staying responsive to user interjections — while the
// ~85s exploration runs, rather than blocking the whole turn before the loop even starts. Only a
// plan with NO write step (delegate/refine) is eligible; a mixed plan keeps the synchronous
// executeSteps path so a write step still sees prior explorer findings in its brief (ordering
// dependency). Default ON; MAGI_ASYNC_EXPLORERS=off restores the fully-synchronous preflight (the
// A/B knob). Mirrors specFidelityEnabled's env shape.
func asyncExplorersEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_ASYNC_EXPLORERS"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// sharedRefineEnabled runs a plan's sequentially-dependent refine phases in ONE shared child
// session (the first phase creates it via clone; later phases REUSE it, so each sees its
// predecessors' actual work) rather than giving each phase its own spawn-time clone of the
// parent — the fix for tightly-coupled phases missing each other's outputs. Default ON;
// MAGI_REFINE_SHARED=0 restores the legacy per-phase clone-at-spawn baseline (the A/B knob).
func sharedRefineEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_REFINE_SHARED"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// planConvergeEnabled gates the plan-audit convergence judgment (D17): when the council
// rejects a plan and the planner re-plans, judge whether the revision actually addressed
// the concern and stop the loop early on an unproductive (ignored-the-concern) revision,
// rather than bounding purely on the round count. Default ON; MAGI_PLAN_CONVERGE=0 restores
// the round-count-only behavior (the PlanRevised diff is still emitted, but with no verdict).
func planConvergeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_PLAN_CONVERGE"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// stallConvergeEnabled gates the stalled-nudge convergence (D18a): the no-progress "stalled"
// nudge re-arms up to maxStallNudges times keyed purely on the sinceProgress count, without
// checking whether the redirect actually changed anything. When a re-arm's window produced no
// structural forward motion — neither a real mutation NOR a NOVEL exercising command
// (progressSinceNudge stays false) — the nudge was ignored, so collapse the remaining nudge
// budget and let the stall force-stop land the honest outcome now instead of burning more
// no-progress windows. It only ACCELERATES the same terminal landing (stuck()=="stall"); it
// never forces a pass and never fires while the agent is making progress (a mutation sets
// progressSinceNudge=true and restarts the window, so a post-nudge edit re-arms normally). Default
// ON; MAGI_STALL_CONVERGE=0 restores the fixed maxStallNudges re-arm.
func stallConvergeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_STALL_CONVERGE"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// refineShare threads the shared-session state across a plan's refine phases: the first phase
// pins the child session it created and that session's executor here; later phases reuse both
// so they run in ONE session with a stable agent. Zero value = no shared session yet.
type refineShare struct {
	sid   session.SessionID
	agent string
}

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

// runPlanAuditGate has the council review the PROCEDURE before it runs (D17). It
// returns the procedure to execute — the original (approved or force-approved) or
// a revised one. The pure tally is reused via the council adapter with Phase=plan;
// there is no diff/report/signals, and revise feedback drives a re-plan (it is NOT
// injected into the main session). It has its own bounded rounds.
func (a *App) runPlanAuditGate(ctx context.Context, s session.Session, spec AgentSpec, prompt string, steps []planStep, depth, maxSteps int) []planStep {
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
		// The abstract-vs-absurd distinction for refine plans is made by the member LENS,
		// not a blanket code rule: the lens approves an intentionally high-level refine step
		// (abstractness is not a flaw — it is worked out at execution time) but STILL fires
		// critical for a genuinely unsound plan (wrong approach, a required part missing, a
		// plan that would not achieve the task), even when abstract. A deterministic
		// "refine ⇒ never critical" override was rejected: it would also wave through an
		// absurd plan, which must still be rejected.
		critical := council.HasCriticalRevision(delib.Verdicts)
		advice := strings.TrimSpace(council.AdvisoryFeedback(delib.Verdicts))

		if !critical { // approve, possibly carrying non-blocking advice
			a.storePlanCriteria(ctx, s, delib.Criteria) // the contract for the termination gate
			note := ""
			if advice != "" {
				a.injectCouncilAdvice(ctx, s.ID, advice, true) // accepted: the executor heeds it
				note = "plan approved with advisory notes (non-blocking)"
				if a.cfg.CouncilPlanAbsorb { // option B: fold the advice into the plan now
					if next := sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, advice, depth, maxSteps, "")); len(next) > 0 {
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
		next := sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, fb, depth, maxSteps, ""))
		if len(next) == 0 {
			next = sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, fb, depth, maxSteps, ""))
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

		// Plan-audit convergence (D17): judge whether the revision actually engaged the
		// council's critical concern. A productive revision (addressed) keeps looping to the
		// cap as before; an unproductive one (ignored the concern) stops early — re-planning
		// again tends to repeat the same conclusion, so hand the concern to the executor and
		// let the execution + landing gates arbitrate ("run it to know", the plan-side symmetry
		// of the evidence gate) instead of burning rounds. The before/after diff + critique +
		// verdict are always emitted as a PlanRevised fact, so the revision is observable even
		// when gating is off (Addressed nil then).
		var addressed *bool
		reason := ""
		if planConvergeEnabled() {
			v, jerr := a.cfg.Council.JudgeRevision(ctx, port.RevisionJudgeRequest{
				Critique: fb, PriorPlan: renderSteps(steps), RevisedPlan: renderSteps(next), DefaultModel: s.Model.Model,
			})
			if jerr != nil { // fail open — a flaky judge must never cut a productive loop
				v = port.RevisionVerdict{Addressed: true, Reason: "revision judge error: " + jerr.Error()}
			}
			ok := v.Addressed
			addressed = &ok
			reason = v.Reason
		}
		pr, _ := json.Marshal(event.PlanRevisedData{
			Round: round, Critique: fb, Before: stepSummaries(steps), After: stepSummaries(next),
			Addressed: addressed, Reason: reason,
		})
		a.appendFact(ctx, sid, event.TypePlanRevised, actor, pr)

		if addressed != nil && !*addressed {
			// Unproductive re-plan → stop early. Proceed with the revised plan but hand the
			// executor the unaddressed concern (execution + landing gates arbitrate).
			a.storePlanCriteria(ctx, s, delib.Criteria)
			a.injectCouncilAdvice(ctx, s.ID, fb, false)
			dd3, _ := json.Marshal(event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown,
				Note:     fmt.Sprintf("plan revision did not address the concern after %d round(s) — proceeding (execution + landing gates arbitrate)", round),
				Criteria: delib.Criteria,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd3)
			return next
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

// executeSteps runs each step by its strategy, accumulating explorer findings.
// A per-turn explorer budget caps total dispatch; a step that can't dispatch
// (solo, or a scout/parallel that yields nothing) degrades to "the main agent
// handles it" without aborting the procedure.
func (a *App) executeSteps(ctx context.Context, s session.Session, goal string, steps []planStep, depth int) (findings string, delegated bool) {
	budget := maxPlanExplorers
	stepCtx := !stepContextDisabled() // A/B: off → delegate/fan-out run context-free (pre-brief baseline)
	var rshare refineShare            // shared-session state carried across this plan's refine phases
	var out []string
	for i, st := range steps {
		if budget <= 0 || ctx.Err() != nil {
			break
		}
		// Write-capable steps (delegate, refine) are dispatched by the same caller glue — both
		// run inline in this sequential loop (never fanned out) so their writes can't race the
		// council's change capture (see allParallelSafe), and both re-plan at depth+1. They
		// differ only in the child's context and retry model: delegate hands off a self-contained
		// sub-task context-free; refine works an in-context sub-goal with the parent CLONED in.
		// The strategy selects the runner; the record-finding/OR-delegated glue is shared.
		if run := a.writeStepRunner(st.Strategy); run != nil {
			brief := ""
			if st.Strategy == "delegate" && stepCtx {
				brief = delegateBrief(goal, steps, i, out) // refine ignores this (it clones context)
			}
			if f, done := run(ctx, s, st, brief, i, depth, &budget, &rshare); f != "" {
				out = append(out, f)
				delegated = delegated || done
			}
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
		fanGoal := ""
		if stepCtx {
			fanGoal = goal // orient read-only explorers with the overall goal (no sibling outputs — they produce none)
		}
		if f := strings.TrimSpace(a.runExplorers(ctx, s, groups, fanGoal, depth)); f != "" {
			out = append(out, stepFinding(st.Title, "", f))
			a.completeThrough(ctx, s.ID, plannerActor, i) // step i done
		} else {
			a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending") // degraded → don't leave a stuck ◐
		}
	}
	return strings.Join(out, "\n\n"), delegated
}

// stepFinding formats a step's recorded finding as a "### Title (status)" header followed by
// the body — the single shape every write-step and explorer result uses. status is the
// parenthetical tag ("refined", "delegated to coder", "refine FAILED …"); pass "" for a bare
// "### Title" header (the explorer/parallel case).
func stepFinding(title, status, body string) string {
	h := "### " + title
	if status != "" {
		h += " (" + status + ")"
	}
	return h + "\n" + body
}

// resolveWriteExecutor picks the write-capable agent to run a write-step (delegate or refine).
// A named, valid delegatable agent always wins. When the step named none (or a read-only/unknown
// one) and fallbackAny is set, it falls back to the first delegatable agent — this is refine's
// contract (its "agent" is OPTIONAL, since a refine step states a high-level GOAL, not who runs
// it; the CLONED context carries the sub-goal, not the executor identity). delegate passes
// fallbackAny=false: it requires a named executor. ok=false → no executor → degrade to solo.
func (a *App) resolveWriteExecutor(stAgent string, fallbackAny bool) (string, bool) {
	if name, ok := a.delegateAgentName(stAgent); ok {
		return name, true
	}
	if fallbackAny {
		if names := a.delegatableAgents(); len(names) > 0 {
			return names[0], true
		}
	}
	return "", false
}

// writeStepFn runs one write-capable step (delegate or refine): it returns the finding to
// record and done=true when the write actually landed. Both runners share this signature so
// executeSteps dispatches them through one path (the record-finding / OR-delegated glue).
// brief is the delegate context brief (see delegateBrief); refine ignores it (it clones the
// parent context instead), so the caller passes "" for refine steps.
// rs threads the refine shared-session state across a plan's phases; delegate ignores it.
type writeStepFn func(ctx context.Context, s session.Session, st planStep, brief string, i, depth int, budget *int, rs *refineShare) (finding string, done bool)

// writeStepRunner maps a write-capable strategy to its runner, or nil for a strategy this
// method does not own (parallel/scout/solo fall through to explorer/degrade handling).
func (a *App) writeStepRunner(strategy string) writeStepFn {
	switch strategy {
	case "delegate":
		return a.runDelegateStep
	case "refine":
		return a.runRefineStep
	}
	return nil
}

// runDelegateStep dispatches one delegate step: hand its self-contained sub-task to a
// write-capable executor that re-plans at depth+1. It charges *budget per dispatch (like
// an explorer) and returns the finding to record plus done=true when the write actually
// landed — the caller ORs that into its delegated flag. An empty finding means the step
// degraded to solo (no valid executor); the caller records nothing and the main agent
// handles that work. Sequential by construction (never fanned out), so the writes can't
// race the council's change capture — see allParallelSafe.
func (a *App) runDelegateStep(ctx context.Context, s session.Session, st planStep, brief string, i, depth int, budget *int, _ *refineShare) (finding string, done bool) {
	agentName, ok := a.resolveWriteExecutor(st.Agent, false) // delegate requires a named executor
	if !ok {
		return "", false // no valid executor → degrade to solo (the main agent does it)
	}
	*budget-- // count against the per-turn dispatch budget like an explorer
	a.advanceTo(ctx, s.ID, plannerActor, i)
	r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: delegatePrompt(st, brief), PlanStepIndex: &i})
	text := strings.TrimSpace(r.Text)
	// ADaPT failure branch (reactive, as-needed decomposition): a hard failure (spawn error
	// or empty result), while we're still below the plan-depth cap and have budget, gets ONE
	// retry that tells the SAME executor to DECOMPOSE the sub-task into smaller independent
	// steps. The child re-plans at depth+1 (this is the natural decomposition point — it plans
	// from the Task), so a monolithic attempt that failed can succeed piece by piece. Single
	// attempt — bounded by the depth gate and the shared budget. Gated by MAGI_ADAPT: with it
	// off, a failed delegate backtracks after one shot (planned decomposition only).
	if !adaptDisabled() && (r.Err != "" || text == "") && depth+1 < a.cfg.MaxPlanDepth && *budget > 0 {
		*budget--
		r = a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: redecomposePrompt(st, brief), PlanStepIndex: &i})
		text = strings.TrimSpace(r.Text)
	}
	if r.Err != "" || text == "" {
		// Still failed → the sub-task is NOT done. Leave its todo pending so the main
		// agent picks it up, and record it as FAILED (never "(delegated to …)", so the
		// redo-prevention directive can't tell the agent to skip re-doing it).
		note := "the delegated sub-agent returned no result"
		if r.Err != "" {
			note = "the delegated sub-agent errored: " + r.Err
		}
		a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
		return stepFinding(st.Title, "delegate FAILED — do this yourself", "("+note+"; this sub-task is unfinished)"), false
	}
	a.completeThrough(ctx, s.ID, plannerActor, i)
	return stepFinding(st.Title, "delegated to "+agentName, text), true
}

// refineLocalRetries bounds how many INFORMED local attempts a refine node gets before it
// is declared exhausted and backtracks to the parent. Small on purpose: each attempt is a
// full child run, and a weak model must not thrash one node indefinitely. Also bounded by
// the shared per-turn dispatch budget and the depth cap.
const refineLocalRetries = 2

// runRefineStep executes one hierarchical refine step: a large, NON-independent sub-goal
// worked out IN-CONTEXT. Unlike delegate's context-free hand-off, the sub-goal is re-planned at
// depth+1 with the full context carried forward. By default (sharedRefineEnabled) a plan's
// refine phases share ONE child session: the first phase seeds it by CLONING the parent, and
// later phases REUSE it (ReuseSession) so each sees its predecessors' actual work; with
// MAGI_REFINE_SHARED=0 every phase instead gets its own spawn-time clone. It drives the local
// re-plan / escalate loop the hierarchical model needs:
//   - success   → the child's writes are already in the shared tree; complete the todo and
//     return done=true (the caller ORs it into `delegated`, so the depth-0 review gate
//     verifies the merged result).
//   - failure   → record the failure back into the PARENT context and retry the node
//     locally. The failure reason is prefixed onto the retry prompt so the attempt is informed
//     ("a previous attempt failed because X"); under the shared session the retry also runs on
//     top of the failed attempt's actual conversation.
//   - exhausted → leave the todo pending, return a FAILED finding and done=false. The
//     failures now stand in the parent context, so the parent (itself possibly a refine
//     node) re-approaches with them in view — the "no more to try → backtrack up" step.
//
// An explicit STATUS: FAILED report from the child backtracks EARLY (its own accumulated
// failures told it the node is hopeless), without spending the remaining local retries.
// The executor is the step's own agent if it named one, else any delegatable agent; refine
// degrades to solo (the main agent works it out in-context) only when NONE is available.
func (a *App) runRefineStep(ctx context.Context, s session.Session, st planStep, _ string, i, depth int, budget *int, rs *refineShare) (finding string, done bool) {
	// The brief arg is ignored: refine spawns a CLONED-context child (CloneContext below), so
	// the parent goal, prior refine seeds (recordRefineSuccess), and sibling notes already ride
	// in the cloned conversation — no separate brief is needed or wanted.
	// A refine step usually names NO executor (its "agent" is optional — see resolveWriteExecutor),
	// so fall back to any delegatable agent; the CLONED context, not the executor identity, carries
	// the sub-goal. Degrade to solo only when no delegatable agent exists at all.
	agentName, ok := a.resolveWriteExecutor(st.Agent, true)
	if !ok {
		return "", false
	}
	// Shared-session (default): once the first phase has created the shared child, later phases
	// reuse it AND its executor, so the run stays in ONE session with a stable agent.
	shared := sharedRefineEnabled()
	if shared && rs.sid != "" && rs.agent != "" {
		agentName = rs.agent
	}
	a.advanceTo(ctx, s.ID, plannerActor, i)
	// Reactive (as-needed) informed retries are the ADaPT mechanism; MAGI_ADAPT=0 cuts them to a
	// single shot, so a failed refine node backtracks to the parent instead of re-attempting.
	retries := refineLocalRetries
	if adaptDisabled() {
		retries = 1
	}
	fail := ""
	for attempt := 0; attempt < retries && *budget > 0; attempt++ {
		*budget-- // count each attempt against the per-turn dispatch budget, like an explorer
		req := port.SpawnRequest{Agent: agentName, Prompt: refinePrompt(st, fail), CloneContext: true, PlanStepIndex: &i}
		if shared && rs.sid != "" {
			// Reuse the shared child instead of re-cloning the parent: this phase (or retry)
			// runs on top of its predecessors' ACTUAL conversation, not a spawn-time snapshot.
			req.ReuseSession = rs.sid
			req.CloneContext = false
		}
		r := a.spawn(ctx, s, depth, req)
		if shared && r.SessionID != "" {
			// Pin the shared session + its executor for later phases. Assigned every attempt so a
			// reuse miss (fresh id returned) self-heals onto the new session; on a normal reuse
			// r.SessionID is the same id, so this is a no-op.
			rs.sid, rs.agent = r.SessionID, agentName
		}
		text := strings.TrimSpace(r.Text)
		if r.Err == "" && text != "" && !refineReportsFailure(text) {
			a.completeThrough(ctx, s.ID, plannerActor, i)
			// Seed the parent (main-agent) context with this phase's output. Under the shared
			// session siblings already see each other's ACTUAL work; this note is the summary the
			// PARENT reads (and the sibling-visibility fallback when MAGI_REFINE_SHARED=0).
			a.recordRefineSuccess(ctx, s.ID, st, text)
			return stepFinding(st.Title, "refined", text), true
		}
		// Record the failure into the PARENT context: the next clone carries it (informed
		// retry), and on exhaustion the parent backtracks with it in view.
		fail = refineFailReason(r, text)
		a.recordRefineFailure(ctx, s.ID, st, fail)
		if r.Err == "" && refineReportsFailure(text) {
			break // the child judged the node hopeless → backtrack now, don't burn retries
		}
	}
	a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
	return stepFinding(st.Title, "refine FAILED after local retries — reconsider the approach yourself", "("+fail+"; this sub-goal is unfinished)"), false
}

// refinePrompt frames a refine step as an in-context sub-goal. On a local retry it leads
// with the prior failure so the next attempt changes approach (the failure is also in the
// cloned context, but stating it explicitly steadies a weak model).
func refinePrompt(st planStep, fail string) string {
	p := ""
	if f := strings.TrimSpace(fail); f != "" {
		p = "A previous attempt at this sub-goal did NOT succeed: " + f + "\nTake a DIFFERENT approach this time.\n\n"
	}
	return p + st.Task + "\n\n(You are working out ONE sub-goal of a larger plan, continuing from the conversation " +
		"so far. Break it into concrete steps as needed, complete it fully, then " + noFabricate + " If after real " +
		"effort this sub-goal genuinely cannot be done, report status \"failed\" and say plainly what blocked you — " +
		"do not report unfinished work as done.)"
}

// noFabricate is the anti-fabrication half of every child hand-off's self-verify contract:
// verify by real execution and cite it, and if you could NOT run/confirm something, admit it
// rather than manufacture a verified-looking result. The delegate and stuck-recovery hand-offs
// previously asked only to "report how you verified it" with no license for the honest negative
// — an asymmetry that pressures a weak model to fabricate (write a stand-in results file it never
// produced) just to answer the ask. Single-sourced so all hand-offs stay symmetric.
const noFabricate = "verify it yourself by actually running it, and report concretely how you verified it (the " +
	"command you ran and its real output). If you could NOT actually run or confirm something, say so plainly and " +
	"treat it as unverified — never invent or hand-write output, and never write a stand-in or placeholder file to " +
	"make it look done; hiding the gap is worse than admitting it."

// refineReportsFailure reports whether the child explicitly declared the sub-goal failed
// (a "STATUS: FAILED" report frame — see subReport.result in app.go). This is the child's
// own "no viable approach" verdict, used to backtrack early.
func refineReportsFailure(text string) bool {
	line, _, _ := strings.Cut(strings.TrimLeft(text, "\n"), "\n")
	return reportStatusWord(line) == "FAILED"
}

// refineFailReason summarizes why a refine attempt failed, for the parent-context failure
// note and the next retry's prompt.
func refineFailReason(r port.SpawnResult, text string) string {
	switch {
	case r.Err != "":
		return "the attempt errored: " + r.Err
	case text == "":
		return "the attempt produced no result"
	case refineReportsFailure(text):
		if reason := strings.TrimSpace(stripReportStatus(text)); reason != "" {
			return "the attempt reported failure: " + clipLine(reason, 500)
		}
		return "the attempt reported failure"
	default:
		return "the attempt did not complete the sub-goal"
	}
}

// recordRefineFailure appends a refine node's failure into the PARENT session as an
// agent-authored note, so it enters the parent's context: the next local retry re-clones
// the parent and therefore sees it, and on escalation the parent re-approaches with it in
// view. This accumulating failure record is what the hierarchical backtracking relies on.
func (a *App) recordRefineFailure(ctx context.Context, sid session.SessionID, st planStep, reason string) {
	note := "Sub-goal not yet achieved — \"" + strings.TrimSpace(st.Title) + "\": " + reason
	_ = a.appendPromptText(context.WithoutCancel(ctx), sid,
		event.Actor{Kind: event.ActorAgent, ID: plannerAgent}, note)
}

// recordRefineSuccess is the SUCCESS mirror of recordRefineFailure: it appends a completed
// refine node's result into the PARENT session as an agent-authored note. This is what makes
// sequentially-dependent refine phases cohere. Each refine child's conversation is CLONED
// from the parent AT SPAWN TIME (CloneContext), and executeSteps only injects the batched
// findings once the whole loop is done — so without this, phase N spawns with a clone that
// predates phase N-1's output and can't build on it (mismatched packages/signatures, import
// cycles). Writing a compact completion note here seeds the next clone with what the prior
// phase produced. The result is clipped: the real code is already on disk, so the note only
// needs to carry the narrative (what was built, key names/signatures), not the transcript.
func (a *App) recordRefineSuccess(ctx context.Context, sid session.SessionID, st planStep, result string) {
	note := "Sub-goal completed — \"" + strings.TrimSpace(st.Title) + "\": " + clipLine(strings.TrimSpace(result), 800)
	_ = a.appendPromptText(context.WithoutCancel(ctx), sid,
		event.Actor{Kind: event.ActorAgent, ID: plannerAgent}, note)
}

// redecomposeStuck is the ADaPT failure-branch for a SOLO agent that got stuck — the same
// "BREAK IT DOWN and re-plan" recovery as runDelegateStep, but triggered mid-run when a solo
// attempt thrashed (stall guard exhausted) or deadlocked (council never approved) instead of
// on a delegated child's failure. It hands the ORIGINAL task, plus the concrete reason the
// last attempt was stuck, to a fresh write-capable executor that re-plans at depth+1 and
// continues from the partial work already on disk. The caller gates it to fire at most once
// per run and only for a plan-eligible (write-capable, below the depth cap) agent, so it can
// never recurse unboundedly or fire for a read-only leaf. Returns true when the child produced
// a result to integrate (injected as findings, so the parent verifies the merged output rather
// than blindly re-running); false when no executor is available or the child also failed, so
// the caller falls through to its existing force-stop/finish.
func (a *App) redecomposeStuck(ctx context.Context, s session.Session, agent AgentSpec, task, blockReason string, depth int) bool {
	// Re-spawn the same write-capable agent (planEligible guarantees it produces files),
	// mirroring runDelegateStep's same-executor retry; fall back to any delegatable agent.
	name, ok := a.delegateAgentName(agent.Name)
	if !ok {
		names := a.delegatableAgents()
		if len(names) == 0 {
			return false // no valid executor → cannot recover, let the caller stop
		}
		name = names[0]
	}
	r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: name, Prompt: stuckRedecomposePrompt(task, blockReason)})
	if r.Err != "" || strings.TrimSpace(r.Text) == "" {
		return false
	}
	a.injectSubagentResult(ctx, s.ID, name, r)
	return true
}

// stuckRedecomposePrompt frames the solo-stuck recovery: the decompose instruction, the task,
// and the specific wall the previous attempt hit (a stall reason or the council's last unmet
// concern) so the child knows what to break through, then the delegate contract's self-verify
// framing. Reuses decomposePrefix so its "BREAK IT DOWN" wording stays single-sourced.
func stuckRedecomposePrompt(task, blockReason string) string {
	p := decomposePrefix + strings.TrimSpace(task)
	if r := strings.TrimSpace(blockReason); r != "" {
		p += "\n\nWhat blocked the previous attempt (address this directly): " + r
	}
	return p + "\n\n(You are taking over a task a previous attempt got stuck on; partial work may " +
		"already be on disk. Complete it fully, then " + noFabricate + ")"
}

// delegatePrompt frames a delegate step as a self-contained sub-task instruction, optionally
// prefixed with a compact brief (see delegateBrief). The brief is orientation only — the task
// itself stays self-contained, so an empty brief leaves the original context-free hand-off.
func delegatePrompt(st planStep, brief string) string {
	p := ""
	if b := strings.TrimSpace(brief); b != "" {
		p = b + "\n\n"
	}
	return p + st.Task + "\n\n(You are handling ONE independent part of a larger plan. Complete this part fully, " +
		"then " + noFabricate + ")"
}

// delegateBrief builds the compact context a delegate child gets IN ADDITION to its
// self-contained task: the overall goal (so its choices align with the whole task, not just
// its slice), the OTHER step titles (boundaries — what it must NOT redo), and what earlier
// steps ALREADY produced (interfaces/outputs to build on rather than reinvent). It carries
// NO parent conversation or reasoning — that is refine's job (a full context clone); a
// delegate child stays a clean-room worker that just knows where its slice fits. Every part
// is clipped so the brief can't blow up as siblings accumulate. Returns "" when there is
// nothing worth briefing (e.g. a lone first step with no goal text).
func delegateBrief(goal string, steps []planStep, i int, prior []string) string {
	var b strings.Builder
	if g := strings.TrimSpace(goal); g != "" {
		// Part C: the delegate child is context-free — it never sees the raw request, so a
		// paraphrased goal is its ONLY spec. When spec fidelity is on, carry the goal verbatim
		// (generously clipped) and label it authoritative, so the child copies exact identifiers
		// from it rather than normalizing them. Off → the compact 400-char orientation line.
		// The cap is generous (8000B covers virtually every real request) and, crucially, uses
		// clipSpec — a bare "…" here made the child reproduce a truncated block into an edit
		// old-string that then matched nothing (the exact defect the VERBATIM label promises against).
		if specFidelityEnabled() {
			b.WriteString("SPEC (authoritative — for any exact name, field, format, or value, follow this VERBATIM): " + clipSpec(g, 8000) + "\n")
		} else {
			b.WriteString("Overall goal (the whole task your part serves): " + clipLine(g, 400) + "\n")
		}
	}
	var others []string
	for j, st := range steps {
		if j != i {
			if t := strings.TrimSpace(st.Title); t != "" {
				others = append(others, t)
			}
		}
	}
	if len(others) > 0 {
		b.WriteString("Other steps handled separately (do NOT redo these): " + clipLine(strings.Join(others, "; "), 400) + "\n")
	}
	if p := strings.TrimSpace(strings.Join(prior, "\n")); p != "" {
		b.WriteString("Already produced by earlier steps — build on these, don't rebuild:\n" + clipLine(p, 800) + "\n")
	}
	return strings.TrimSpace(b.String())
}

// decomposePrefix leads the ADaPT failure-branch retry: the direct attempt did not
// complete, so tell the executor to break the sub-task down and do the pieces one at a
// time. The executor re-plans at depth+1, so this decomposition instruction lands in its
// own pre-flight planner.
const decomposePrefix = "A direct attempt at the task below did NOT complete. Approach it differently now: BREAK IT DOWN " +
	"into smaller, independent steps and carry them out ONE AT A TIME, verifying each before moving on.\n\n"

// redecomposePrompt frames the ADaPT failure-branch retry as the delegate instruction with
// the decompose prefix — it reuses delegatePrompt's self-contained framing, so the two
// share their trailing contract instead of duplicating it.
func redecomposePrompt(st planStep, brief string) string {
	return decomposePrefix + delegatePrompt(st, brief)
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
// exact two-field frame is removed (via reportStatusWord, which matches the "STATUS:"
// keyword case-insensitively — the sole producer emits it upper-case), so a legitimate
// first item that merely starts with "STATUS:" (multi-word) or a path is never touched.
func stripReportStatus(text string) string {
	text = strings.TrimLeft(text, "\n")
	if line, rest, ok := strings.Cut(text, "\n"); ok && reportStatusWord(line) != "" {
		return rest
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
// explorerPrompt builds the read-only investigation prompt for one explorer group, optionally
// prefixed with the overall goal for orientation. Shared by the synchronous (runExplorers) and
// background (dispatchExplorerSteps) fan-out paths so both send an identical prompt.
func explorerPrompt(goal string, g planGroup) string {
	prompt := fmt.Sprintf("Investigate (read-only): %s\n\n%s", g.Focus, g.Question)
	if og := strings.TrimSpace(goal); og != "" {
		prompt = "Overall goal (context for your investigation): " + clipLine(og, 400) + "\n\n" + prompt
	}
	return prompt
}

func (a *App) runExplorers(ctx context.Context, s session.Session, groups []planGroup, goal string, depth int) string {
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
			prompt := explorerPrompt(goal, g)
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
	_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "council"}, text)
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
