package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// run is the async entry for a top-level Submit: it loads the session's agent
// and drives the loop, returning any terminal error (e.g. a provider failure) so
// the caller can avoid re-running a failed turn into a retry storm.
func (a *App) run(ctx context.Context, sid session.SessionID) error {
	s := a.sessionInfo(ctx, sid)
	if a.cfg.Workflow {
		return a.runWorkflow(ctx, s)
	}
	// The pre-flight planner now runs INSIDE runLoop (loop.go), so a delegated
	// sub-task re-plans at its own level (recursive planning). runLoop is the single
	// entry point — planning here would double-plan the top level.
	agent := a.agentFor(s)
	_, err := a.runLoop(ctx, s, agent, 0, 0, false)
	return err
}

// buildStepSystem assembles the cacheable system prompt for one loop step: the
// base agent/project prompt, an optional primacy language-lock directive (top
// level only), and the static list of available skills. Kept byte-stable within
// a turn so the backend's prefix (KV) cache survives across steps — per-step
// volatile context (plan/experience/RAG) is injected separately, never here.
func (a *App) buildStepSystem(agent AgentSpec, workdir string, isSub bool, evs []event.Event) string {
	sys := a.systemFor(agent, workdir, isSub)
	// Language lock: weak models ignore a "reply in the user's language" rule
	// buried in a long prompt, so detect the user's script and put a short,
	// forceful directive FIRST (primacy). Top-level only — subagents report to
	// the orchestrator, not the user. Lock to the genuine user's language, NOT the
	// latest user-role message — council/hook/auto feedback is injected as a
	// user-role prompt (often English), which would let a weak model drift.
	if !isSub {
		if dir := langDirective(lastUserPromptText(evs)); dir != "" {
			sys = dir + "\n\n" + sys
		}
	}
	// Available skills (model loads one via the skill tool when relevant).
	if sk := a.loadSkills(workdir); len(sk) > 0 {
		var b strings.Builder
		b.WriteString("\n\n# Available skills (use the skill tool to load one)\n")
		for _, x := range sk {
			b.WriteString("- " + x.Name + ": " + oneLineHint(x.Description) + "\n")
		}
		sys += strings.TrimRight(b.String(), "\n")
	}
	return sys
}

// loopAction is what the step loop does after a no-tool-call finish attempt
// (finishTurn), which has several exits: keep looping (feedback injected, parked,
// or stuck-recovered), re-enter without spending a step, finish the turn, or
// unwind a cancellation. Returning an action keeps the branch decision with the
// step loop that owns step/lastText, so finishTurn stays a pure decision.
type loopAction int

const (
	loopContinue  loopAction = iota // re-enter the step loop (feedback injected / parked / recovered)
	loopRetryStep                   // re-enter WITHOUT consuming a step (step--)
	loopFinish                      // the turn is done → return the result
	loopAbort                       // context cancelled → return ctx.Err()
)

// turnState is the per-turn mutable bookkeeping the step loop carries across steps
// and hands to finishTurn: the once-per-turn guards (stop hooks, empty-subagent
// nudge), the council's rejected-answer memory (to short-circuit an unchanged
// resubmit), the UNVERIFIED reason a non-approving finish carries, the one-shot
// stuck-recovery flag (shared with the stall path), and the consensus gate's own
// round accounting. reground zeroes the turn-scoped fields; council.spent survives
// (it is the turn's cumulative deliberation clock — see reground).
type turnState struct {
	stopChecked      bool        // stop hooks enforced at most once per turn
	nudgedEmpty      bool        // empty-subagent "call report" nudge fired at most once
	execNudged       bool        // authored-but-never-executed nudge fired at most once (exec-evidence)
	prevFinishText   string      // the answer the council rejected last round
	prevFinishCalls  int         // guard.callCount() at that rejection (-1 = none yet)
	unverifiedReason string      // non-empty when the turn finishes WITHOUT council approval
	recovered        bool        // stuck-recovery redecompose fired at most once (shared with the stall path)
	stepNudged       bool        // deliverable-check failure nudge injected at most once (MAGI_STEP_VERIFY)
	council          councilTurn // consensus gate rounds/feedback/spent/deadlock (D14)
}

// turnCtx bundles the values that are fixed for the whole turn — the session, the
// running agent, its nesting depth and step budget, the agent's event actor, the
// turn's start clock, and the run guard. They are threaded together (rather than as
// a long parameter list) into the finish path so finishTurn's signature carries only
// the step-varying inputs alongside this one bundle. guard is a pointer, so its
// mutations propagate; the rest are read-only per turn.
type turnCtx struct {
	s        session.Session
	agent    AgentSpec
	depth    int
	maxSteps int
	actor    event.Actor
	runStart time.Time
	guard    *runGuard
}

// runLoop drives the agent loop until the model stops, max steps are reached, or
// the run is interrupted. It returns the final assistant text (used as a
// subagent's result). depth is the orchestration nesting level (D7); maxSteps<=0
// uses the configured default (the workflow engine passes per-phase budgets).
// (F-LOOP)
func (a *App) runLoop(ctx context.Context, s session.Session, agent AgentSpec, depth, maxSteps int, seedWork bool) (string, error) {
	if maxSteps <= 0 {
		maxSteps = a.cfg.MaxSteps
	}
	sid := s.ID
	runStart := time.Now() // self-measured wall clock (budget line + council cost control)
	agentActor := event.Actor{Kind: event.ActorAgent, ID: orDefault(agent.Name, "default")}
	lastText := ""
	reportRefused := false // a subagent's unverified "done" report was refused once this run
	guard := newRunGuard()
	guard.stallConverge = stallConvergeEnabled() // D18a: collapse the stalled-nudge re-arm when a redirect produced no forward motion
	ts := turnState{prevFinishCalls: -1}         // per-turn mutable bookkeeping (finish guards, council accounting, stuck-recovery); zeroed field-wise on reground
	tc := turnCtx{s: s, agent: agent, depth: depth, maxSteps: maxSteps, actor: agentActor, runStart: runStart, guard: guard}
	// Run-tree recovery cap: a child spawned by the stuck-recovery lifeline starts already
	// flagged as recovered, so it cannot fire its OWN redecomposeStuck. reground does NOT clear
	// this field, so the cap holds across the child's whole run — exactly one recovery executor
	// per run tree rather than one per depth level. Gated so flag-off is the unchanged baseline.
	if recoveryRunCapEnabled() {
		a.mu.Lock()
		if st, ok := a.stateIf(sid); ok && st.recoverySeed {
			ts.recovered = true
		}
		a.mu.Unlock()
	}
	turnTask := "" // the user instruction THIS turn answers, snapshotted at step 0. A
	// steer that lands mid-turn is QUEUED by default (runs as its own follow-up turn), so
	// it can't silently hijack what the council judges against — unless the agent explicitly
	// routes it. A "redirect" re-snapshots turnTask and rebuilds the plan (the goal changed);
	// an "append" folds the steer into turnTask (so the termination gate still enforces it)
	// but FREEZES the plan — the steer is injected as a constraint, not re-decomposed.
	usedTools := seedWork   // did this turn do real work? (planner investigation seeds it; council skips pure conversational turns)
	handledUserPrompts := 0 // genuine (ActorUser) prompts already absorbed into turnTask; a rise past this at step>0 is a mid-turn interjection
	replanCount := 0        // agent-initiated replans honored this turn (budget-capped so replan can't indefinitely reset the stall guard)
	replanAtCalls := -1     // guard.callCount() at the last honored replan (require real work between replans)
	seeded := false         // step-0 turnTask seed ran once; a park-and-retry (step--) must not re-seed/re-enqueue
	// Turn usage accumulation (§8.1): output tokens and cost sum across steps; input
	// is the last step's (the current context size, not a sum).
	var cumOut, lastIn int
	var cumCost float64

	// Pre-flight planner (D17), recursive: for a producing agent below the plan-depth
	// cap, decompose the request into ordered steps, fan out read-only explorers, and/or
	// DELEGATE large independent sub-tasks to sub-agents that re-plan at depth+1. This is
	// the single planning entry point — top level and every delegated sub-task take the
	// same path, so a big task splits recursively (heterogeneous: each node picks solo/
	// parallel/scout/delegate). Read-only explorers/verifiers and workflow mode are gated
	// out. Injects findings before the agent runs; degrades to solo on any failure.
	// Per-turn contract reset at the TURN's start, not only at Submit: a turn can
	// also begin via Steer-after-finish, the run goroutine's exit-window re-run, or
	// a resurfaced queued interjection — none of which pass through Submit. Without
	// this, such a turn inherits the PREVIOUS task's todos/criteria, and the
	// council, planner, and nudges keep citing the old request as the live contract
	// ("the user asked to commit" haunting every later turn). It must run BEFORE
	// the planner preflight — the reset clears awaitExplorers, which the planner's
	// async fan-out is about to set for THIS turn. seedWork marks a caller that
	// already staged this turn's work (dispatched explorers, set the park) before
	// entering the loop — resetting would wipe that staging, so skip it.
	if depth == 0 && !a.cfg.Workflow && !seedWork {
		a.resetForNewTopLevel(sid)
	}
	// Explore-first grounding (opt-in): on the first cold, write-capable top-level turn, land the
	// workspace's build/verify anchors and layout into the main context BEFORE planning, so both
	// the executor and the planner (which reads the session window) start grounded in the real
	// environment. Once per session (maybeOrient's grounded flag), deterministic, hard-bounded.
	if orientEnabled() && depth == 0 && !a.cfg.Workflow && a.planEligible(agent, depth) {
		a.maybeOrient(ctx, s)
	}
	if a.planEligible(agent, depth) {
		// The planner running is NOT itself work: it decides solo-vs-fan-out and may
		// register todos, but authors nothing. Only real tool execution (below) seeds
		// usedTools, so the termination council convenes on turns that actually did
		// something — not on every turn merely because the planner preflight fired.
		a.maybePlanPreflight(ctx, s, depth, maxSteps, "")
	}
	// Show the agent working the next step (◐) for the rest of the turn — a deterministic
	// in_progress signal, since a weak model rarely calls todowrite (no-op if no todos).
	// Skipped in workflow mode, where the deterministic engine owns the plan panel.
	if !a.cfg.Workflow {
		a.markFirstPendingActive(ctx, sid, agentActor)
	}

	// Deterministic plan finalize (top level only): when the turn ends, resolve any
	// unfinished todos — completed if the turn genuinely finished, else cancelled — so
	// the panel reflects the outcome without relying on the model's todowrite. The defer
	// covers every exit (abort, loop-guard, max-steps, panic); WithoutCancel so it still
	// emits after a cancellation. `finished` is set true ONLY at the genuine-done returns.
	finished := false
	if depth == 0 {
		defer func() { a.finalizeTodos(context.WithoutCancel(ctx), sid, finished) }()
	}

	// reground resets the turn's termination/stall accounting so a freshly-adopted task
	// (redirect/append) or plan (replan) isn't instantly force-stopped by the previous
	// approach's accumulated no-progress count. rebuildPlan additionally re-runs the
	// pre-flight planner for a fresh decomposition — used ONLY when the goal changes
	// (redirect) or the agent explicitly replans (honorReplan). An "append" steer does
	// NOT rebuild: the approved plan is frozen for the turn and the steer is injected as
	// a constraint instead, so a mid-turn add-on can't reopen the plan audit or cancel
	// and re-dispatch the explorers.
	reground := func(rebuildPlan bool) {
		guard.resetStall()
		// Field-wise reset (not ts.council = councilTurn{}): council.spent is the turn's
		// cumulative deliberation clock for the cost cap and must survive a re-ground.
		ts.council.rounds = 0
		ts.council.feedback = ""
		ts.council.deadlocked = false
		ts.prevFinishText = ""
		ts.prevFinishCalls = -1
		ts.stepNudged = false // a re-grounded task gets a fresh deliverable-check nudge budget
		if rebuildPlan && a.planEligible(agent, depth) {
			// Re-plan against the ADOPTED task (turnTask), not the bare last user prompt:
			// after a route_interjection "append" the last prompt is only the steer's
			// constraint, which alone would lose the original goal in the re-decomposition.
			a.maybePlanPreflight(ctx, s, depth, maxSteps, turnTask)
		}
	}

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			return lastText, ctx.Err()
		}
		a.setStage(sid, stageExecute) // tag this iteration's events as execute (D15)

		answerInterjectNow := false // set when this step detects a visible interjection to reply to now

		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			a.emitError(ctx, sid, agentActor, err.Error())
			return lastText, err
		}
		if step == 0 && !seeded {
			seeded = true // guard: a park-and-retry (step--) re-enters at step 0 but must not re-seed
			turnTask, handledUserPrompts = a.seedTurnTask(ctx, tc, evs)
		} else {
			// Drain any control signal a tool left last step (route an interjection, or an
			// agent-initiated replan), applying the reground the loop owns but the tool can't.
			if tc := a.takeTurnControl(sid); tc.route != "" || tc.replan {
				// Absorb a routed interjection now, so it isn't also re-surfaced as its own
				// turn. The route binds to a SPECIFIC queued request (resolveRouteTarget: the
				// id the model named, else the oldest queued), not to lastUserPromptText — so
				// with several interjections piled up none is re-absorbed or cross-applied.
				// redirect re-anchors turnTask and rebuilds the plan; append folds the steer in
				// but FREEZES the plan (constraint injection only) — see applyInterjectRoute.
				// "queue"/"" leaves turnTask untouched; an empty resolve means it was already
				// absorbed, so the route is a no-op.
				if tc.route != "" {
					if mid, it := a.resolveRouteTarget(sid, tc.routeID); it != "" {
						if nt, changed := a.applyInterjectRoute(ctx, sid, tc.route, turnTask, mid, it, reground); changed {
							turnTask = nt
						}
					}
				}
				if tc.replan {
					a.honorReplan(ctx, sid, tc.reason, &replanCount, &replanAtCalls, guard.callCount(), reground)
				}
			}
			handledUserPrompts, answerInterjectNow = a.detectInterjections(ctx, tc, evs, turnTask, handledUserPrompts)
		}

		// Async explorer preflight: when the planner dispatched its read-only explorers to the
		// background (a pure-investigation plan), the orchestrator has nothing to synthesize until
		// they report. Park here — BEFORE running the model — rather than answering with no findings.
		// The park releases the moment a user interjects (needsOrchestratorTurn → lastIsUserSteer),
		// so the loop falls through and answers it inline (dispatching=true above); or when the
		// explorers finish (bgOutstanding hits 0), so it wakes to synthesize. Top level only;
		// consumes no step budget. Mirrors the post-step bg-wait (below the no-tool-call branch).
		// Gated on awaitingExplorers so ordinary background delegation still interleaves the
		// orchestrator's OWN work (it never sets the flag) instead of parking here.
		if depth == 0 && a.awaitingExplorers(sid) && !answerInterjectNow && a.bgOutstanding(sid) > 0 && !a.needsOrchestratorTurn(ctx, sid) {
			// Flush asides that were queued earlier (e.g. during planning) BEFORE we park —
			// otherwise they starve until turn-end (the pendingInterject drain fires only then),
			// which for a long/council-looping turn is minutes. Handle each already-queued item
			// through the same focused handler; if one acts on the work, break the park so the
			// route/cancel takes effect instead of waiting on the explorers.
			if queued := a.pendingInterjectItems(sid); len(queued) > 0 {
				for _, it := range queued {
					if a.handleAside(ctx, agent, s, depth, turnTask, it.MsgID, it.Text) {
						answerInterjectNow = true
					}
				}
				if answerInterjectNow {
					step-- // re-evaluate: skip the park and run a normal step to apply the route
					continue
				}
			}
			for a.bgOutstanding(sid) > 0 && ctx.Err() == nil && !a.needsOrchestratorTurn(ctx, sid) {
				select {
				case <-a.bgWaitChan(sid):
				case <-ctx.Done():
				}
			}
			if ctx.Err() != nil {
				return lastText, ctx.Err()
			}
			if a.bgOutstanding(sid) == 0 {
				a.setAwaitExplorers(sid, false) // all explorers reported → normal synthesis/interleave from here
			}
			if a.needsOrchestratorTurn(ctx, sid) {
				a.bgConsume(sid) // don't re-wake for the same results (re-armed when new ones inject)
			}
			step-- // re-evaluate without spending this step's budget
			continue
		}

		req, evs := a.buildStepRequest(ctx, tc, evs, step, cumOut)

		stepStart := time.Now()
		stream, err := a.providerFor(agent).StreamChat(ctx, req)
		if err != nil {
			a.emitError(ctx, sid, agentActor, err.Error())
			return lastText, err
		}

		msgID := "m_" + newID()
		textPartID := "p_" + newID()
		reasonPartID := "p_" + newID()
		res, serr := a.consumeStream(ctx, sid, agentActor, stream, msgID, textPartID, reasonPartID)
		if serr != nil {
			return lastText, serr
		}
		text, reasoning := res.text, res.reasoning
		toolCalls, usage, textConsumed := res.toolCalls, res.usage, res.textConsumed
		// Accumulate this step's usage into the turn totals (§8.1).
		if usage != nil {
			cumOut += usage.Out
			if usage.In > 0 {
				lastIn = usage.In
			}
			cumCost += a.cfg.Models.Get(s.Model.Model).Cost(usage.In, usage.Out)
		}
		// A cancelled context can end the stream early (empty); report it as an
		// error rather than silently finishing the turn (so interrupts unwind and
		// the supervisor sees a cancellation, not a successful completion).
		if ctx.Err() != nil {
			return lastText, ctx.Err()
		}
		// Feed the elastic subagent cap: one full model round trip (request → stream
		// fully consumed) is the speed signal it budgets attempts against. Recorded
		// only for an intact stream — a timeout/interrupt-censored duration is not a
		// round trip and would bias the cap (timeouts stretch it, Esc shrinks it).
		a.llmLat.record(s.Model.Model, time.Since(stepStart))

		// Persist the assistant message: reasoning (if any), then text, then tool calls.
		if reasoning != "" {
			a.appendPart(ctx, sid, agentActor, msgID, session.RoleAssistant, session.Part{
				ID: reasonPartID, Kind: session.PartReasoning, Text: reasoning,
			})
		}
		if text != "" && !textConsumed {
			lastText = text
			a.appendPart(ctx, sid, agentActor, msgID, session.RoleAssistant, session.Part{
				ID: textPartID, Kind: session.PartText, Text: text,
			})
			// If a subagent is blocked waiting on this orchestrator, its reply IS
			// the answer — route it back so the subagent resumes.
			a.answerPendingAsk(sid, text)
		}
		for _, tc := range toolCalls {
			a.appendPart(ctx, sid, agentActor, msgID, session.RoleAssistant, session.Part{
				ID: "p_" + newID(), Kind: session.PartToolCall, ToolCall: tc,
			})
		}

		// No tool calls → the turn wants to finish. Stop hooks enforce checks
		// (e.g. tests must pass); a failure pushes the agent to keep working.
		if len(toolCalls) == 0 {
			// Turn-cumulative usage (§8.1): out/cost summed across steps, in = last.
			u := event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
			switch a.finishTurn(ctx, tc, step, turnTask, lastText, evs, usedTools, handledUserPrompts, u, &ts) {
			case loopRetryStep:
				step-- // re-woken by a background subagent result: re-enter WITHOUT spending a step
				continue
			case loopContinue:
				continue // feedback injected / nudged / stuck-recovered — keep working
			case loopAbort:
				return lastText, ctx.Err() // cancelled while parked in the bg-wait
			case loopFinish:
				finished = true // the turn is over (approved done, or an honest UNVERIFIED landing)
				return lastText, nil
			}
		}

		// Execute tool calls. When a turn requests several read-only tools, run
		// them concurrently; otherwise (writes, permissioned, or task) keep the
		// deterministic sequential order.
		usedTools = true // this turn did real work → the council gate applies
		if len(toolCalls) > 1 && a.allParallelSafe(toolCalls) {
			var wg sync.WaitGroup
			for _, tc := range toolCalls {
				wg.Add(1)
				go func(tc *session.ToolCall) {
					defer wg.Done()
					a.executeTool(ctx, s, agent, depth, agentActor, tc, guard)
				}(tc)
			}
			wg.Wait()
		} else {
			for _, tc := range toolCalls {
				if ctx.Err() != nil {
					return lastText, ctx.Err()
				}
				a.executeTool(ctx, s, agent, depth, agentActor, tc, guard)
			}
		}

		// Explicit output contract: a subagent that filed a report has delivered its final
		// result and its turn ends now — no more steps, no bash-echo looping. handleReport may
		// refuse an unverified "done" once (loopContinue) or finish the turn (loopFinish, with
		// the result string to return).
		u := event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
		if act, result, handled := a.handleReport(ctx, tc, lastText, u, &reportRefused); handled {
			switch act {
			case loopContinue:
				continue // refused an unverified "done" — pushed back to actually run it
			case loopFinish:
				finished = true // report filed → turn delivered its result
				return result, nil
			}
		}

		// Corrective re-grounding: before any force-stop, give a thrashing agent ONE nudge to
		// re-read the task and change approach — far cheaper than burning the rest of the budget.
		a.injectStuckNudge(ctx, tc, turnTask, evs)

		// Loop/stall/spin force-stop (with the last-resort stuck-recovery lifeline). Returns
		// stop=true when the run must end now: clean=true finishes cleanly (delivered-but-spinning),
		// clean=false aborted with a visible error (genuine thrash). Recovery re-arms the loop
		// internally and returns stop=false so the parent integrates the child's result.
		if stop, clean := a.handleStuckGuard(ctx, tc, turnTask, evs, u, &ts); stop {
			if clean {
				finished = true
			}
			return lastText, nil
		}
	}

	// Max steps reached: stop gracefully.
	d, _ := json.Marshal(event.ErrorData{Message: "max steps reached", Code: "max_steps"})
	a.appendFact(ctx, sid, event.TypeError, event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
	return lastText, nil
}

// seedTurnTask snapshots the turn's task at step 0 and returns it with the baseline user-
// prompt count. turnTask is the prompt that SEEDED this turn — the first genuine user prompt
// not already answered by a previous turn — NOT merely the latest. User prompts that piled up
// after the seed but before execution began (e.g. while a synchronous planning/explorer phase
// held the loop) are interjections: they are masked and queued so they run as their own turns
// and never become what the council judges against. Top level only; a subagent session has no
// ActorUser prompts so seedPromptIdx returns -1 and the latest user text drives the turn.
func (a *App) seedTurnTask(ctx context.Context, tc turnCtx, evs []event.Event) (string, int) {
	sid := tc.s.ID
	entries := userPromptEntries(evs)
	var turnTask string
	if tc.depth == 0 && !a.cfg.Workflow {
		if seed := seedPromptIdx(evs); seed >= 0 && seed < len(entries) {
			turnTask = entries[seed].Text
			a.setActiveSeed(sid, entries[seed].MsgID) // so a cancel can abandon exactly this prompt
			for _, it := range entries[seed+1:] {
				if txt := strings.TrimSpace(it.Text); txt != "" && txt != strings.TrimSpace(turnTask) {
					a.markInterjectSeen(sid, it.MsgID)
					a.enqueueInterject(ctx, sid, it.MsgID, txt)
				}
			}
		} else {
			turnTask = lastUserPromptText(evs)
		}
	} else {
		// Subagent (and workflow) turns: prefer the recorded spawn/unit seed. A
		// CloneContext child's log carries the parent's original user prompts
		// (actors preserved), so the last-ActorUser fallback would anchor the
		// child on a STALE parent request instead of its spawn task.
		if sp := a.seedPromptOf(sid); sp != "" {
			turnTask = sp
		} else {
			turnTask = lastUserPromptText(evs) // the prompt that drove this turn
		}
	}
	return turnTask, len(entries) // baseline; a later rise is a mid-turn interjection
}

// detectInterjections handles a mid-turn user interjection (a new genuine user prompt appeared
// since we last absorbed one), returning the updated handled-prompt count and whether a visible
// interjection must be answered now (break any park). Top level only — subagents aren't steered
// by the user. Every interjection is masked from turnTask/council derivation (markInterjectSeen)
// so it can't swap what the council judges against. How it is handled depends on what the
// orchestrator is doing:
//   - idle-parked waiting on its own explorers (awaitingExplorers): it has no work to interleave,
//     so a soft "MAY answer" directive starves the reply (observed: asides dropped for the whole
//     delegated task). Run handleAside — a focused tool-capable turn that replies to chitchat OR
//     signals a steer (route_interjection to redirect/append/queue, cancel_dispatch to stop
//     now-irrelevant subagents, ask_user to clarify). If it acts on the work we break the park so
//     the next normal step applies the route and re-dispatches with the full toolset.
//   - ordinary background delegation (dispatching but not parked): the orchestrator keeps running
//     its own turns, so let the next working turn answer via the soft directive (answering in a
//     separate turn here would double-reply, and the aside is left visible so it can route it).
//   - otherwise idle: queue it to run as its own turn and tell the agent it is deferred so it
//     stops oscillating (it may still call route_interjection to redirect/append).
func (a *App) detectInterjections(ctx context.Context, tc turnCtx, evs []event.Event, turnTask string, handledUserPrompts int) (int, bool) {
	if tc.depth != 0 || a.cfg.Workflow {
		return handledUserPrompts, false
	}
	s, agent, depth := tc.s, tc.agent, tc.depth
	sid := s.ID
	prompts := userPromptEntries(evs)
	if len(prompts) <= handledUserPrompts {
		return handledUserPrompts, false
	}
	answerNow := false
	dispatching := a.bgOutstanding(sid) > 0
	idleWaiting := dispatching && a.awaitingExplorers(sid) // parked with no work to interleave
	// Handle EVERY user prompt that appeared since the last check, not just the newest: two
	// messages steered in during one long step would otherwise advance the counter past the
	// earlier one, dropping it silently.
	var newest, newestID string
	for _, it := range prompts[handledUserPrompts:] {
		if txt := strings.TrimSpace(it.Text); txt != "" && txt != strings.TrimSpace(turnTask) {
			a.markInterjectSeen(sid, it.MsgID)
			switch {
			case idleWaiting:
				// Enqueue-first so route_interjection (which requires a pending interjection) can
				// fire, then run the focused handler. It consumes a resolved chitchat reply / bare
				// cancel itself and leaves a routed redirect/append queued for the drain to apply.
				a.enqueueInterject(ctx, sid, it.MsgID, txt)
				if a.handleAside(ctx, agent, s, depth, turnTask, it.MsgID, txt) {
					answerNow = true // break the park so the route/cancel takes effect next step
				}
			case !dispatching:
				// Defer: queue it (masked from the live model context too) to run as its own turn.
				a.enqueueInterject(ctx, sid, it.MsgID, txt)
			}
			// Ordinary dispatch (dispatching && !idleWaiting): left visible for the interleaving
			// working turn to answer via the soft directive below.
			newest, newestID = txt, it.MsgID
		}
	}
	// idle-park is fully owned by handleAside above. The directive is only for the other cases
	// (non-dispatch queue notice, ordinary-dispatch soft answer).
	if newest != "" && !idleWaiting {
		a.noteInterjection(sid, turnTask, newestID, newest, dispatching)
		if dispatching {
			// The directive we just appended is the last event, so lastIsUserSteer/
			// needsOrchestratorTurn would read false and the early park below would re-swallow
			// it; skip the park this iteration so the model runs and replies.
			answerNow = true
		}
	}
	return len(prompts), answerNow
}

// buildStepRequest assembles one step's model request: the byte-stable system prompt
// (durable AGENTS.md memory, cached across steps within a turn), context-aware auto-
// compaction, auto-orchestration, and the reconstructed message history with the
// per-step-volatile context (live plan/experience/RAG) appended as an ephemeral trailing
// user message — never persisted, so it stays out of the event log, language lock, and
// council snapshot. Compaction and auto-orchestration can inject events and re-read the
// log, so the possibly-refreshed evs is returned alongside the request. Also publishes the
// live context-usage meter. Extracted from runLoop's step loop; behavior unchanged.
func (a *App) buildStepRequest(ctx context.Context, tc turnCtx, evs []event.Event, step, cumOut int) (port.ChatRequest, []event.Event) {
	s, agent, agentActor := tc.s, tc.agent, tc.actor
	sid := s.ID
	isSub := s.Parent != ""
	sys := a.buildStepSystem(agent, s.Workdir, isSub, evs)

	// Unfiltered reconstruction of the whole log, built ONCE per step and shared by the
	// volatile-context retrieval query and the compaction sizing check below — reconstruct
	// is O(events), so each avoided rebuild matters on long sessions.
	raw := reconstruct(evs)

	// Per-step-volatile context (current plan, shared experience, retrieved RAG): built
	// here but injected as an ephemeral trailing message, NOT into `sys`. `sys` (above) is
	// now byte-stable within a turn, so the backend's prefix cache is reused across steps;
	// only this small block at the tail is re-processed each step.
	// Pending interjection notices ride the volatile block (ephemeral, never persisted):
	// a queued-interjection note lives exactly as long as its interjection stays queued,
	// and the dispatch-case nudge is one-shot — so a resolved interjection can no longer
	// echo into later turns or reload views the way the old persisted directive facts
	// did. Taken ONCE per step (the one-shot is consumed) and re-attached on every vol
	// recompute below, so a compaction refresh doesn't drop it.
	note := a.takeInterjectNotes(sid)
	withNote := func(v string) string {
		if note == "" {
			return v
		}
		if v == "" {
			return note
		}
		return v + "\n\n" + note
	}
	vol := withNote(a.volatileContext(ctx, s, agent, isSub, evs, raw, step, tc.maxSteps, time.Since(tc.runStart)))

	// Context-aware auto-compaction (M6): if the assembled context exceeds the model's
	// window budget, summarize older turns and re-read. Measure against sys+vol so the
	// trigger still accounts for the volatile block (it's only used for sizing here).
	if a.maybeCompact(ctx, s, agent, agentActor, evs, raw, sys+"\n\n"+vol) {
		evs, _ = a.store.Read(ctx, sid, 0)
		raw = reconstruct(evs) // refresh after compaction
		vol = withNote(a.volatileContext(ctx, s, agent, isSub, evs, raw, step, tc.maxSteps, time.Since(tc.runStart)))
	}

	msgs := reconstruct(a.liveEvents(sid, evs))
	// If auto-orchestration fires, it injects a directive as a new event; re-read
	// and rebuild msgs so the directive reaches the model in THIS turn, not the next.
	if a.checkAutoOrchestration(ctx, sid, tc.depth, s.Model.Model, sys, msgs) {
		if evs2, err := a.store.Read(ctx, sid, 0); err == nil {
			evs = evs2
			msgs = reconstruct(a.liveEvents(sid, evs))
			raw = reconstruct(evs)
			vol = withNote(a.volatileContext(ctx, s, agent, isSub, evs, raw, step, tc.maxSteps, time.Since(tc.runStart)))
		}
	}
	// Append the volatile context as an ephemeral trailing user message (not persisted, so
	// it never enters the event log, the language lock, or the council's task snapshot).
	// Placed last for recency and so the entire real prefix stays cacheable. A trailing
	// user message after tool results (and a 2nd user message at step 0) is accepted by
	// OpenAI/Ollama directly; the Anthropic-via-LiteLLM path relies on LiteLLM coalescing
	// consecutive same-role messages.
	if vol != "" {
		msgs = append(msgs, session.Message{Role: session.RoleUser, Parts: []session.Part{{
			Kind: session.PartText,
			Text: "# Runtime context (your live plan and any retrieved references — not a new user instruction)\n" + vol,
		}}})
	}
	a.publishContextUsage(sid, agentActor, s.Model.Model, sys, msgs, cumOut)

	return port.ChatRequest{
		Model:    s.Model.Model,
		System:   sys,
		Messages: msgs,
		Tools:    a.toolSpecs(agent, isSub, tc.depth),
	}, evs
}

// handleReport applies a subagent's filed report (the explicit output contract). It returns
// (action, result, handled): handled=false when no report was filed (fall through to the
// guards). A "done" report whose deliverable was changed but never exercised this turn
// (unverifiedDeliverable, keyed off this subagent's own tool log) is the language-agnostic
// replacement for the report tool's former English confession-phrase scan — refused ONCE
// (loopContinue) so the agent actually runs its work. Gated on the subagent HAVING a way to
// run it (bash): a read/write-only agent cannot execute anything, so blocking it for "not
// running it" would be a false positive — that case defers to the parent's review-gate tester,
// which runs the merged deliverable for real. Otherwise the turn finishes (loopFinish) with the
// report's result. This short-circuits the top-level-only pre-finish gates, so the delegated
// path carries its own verification here.
func (a *App) handleReport(ctx context.Context, tc turnCtx, lastText string, u event.Usage, reportRefused *bool) (loopAction, string, bool) {
	s, agent := tc.s, tc.agent
	sid := s.ID
	rep := a.takeReport(sid)
	if rep == nil {
		return 0, "", false
	}
	if rep.status == "done" && !*reportRefused && agent.allows("bash") && tc.guard.unverifiedDeliverable() {
		*reportRefused = true
		msg := "You reported done, but you changed a deliverable this turn and ran no command that " +
			"exercises the current version, so its behavior is unverified. Actually run the required " +
			"program/command against your result and confirm the REAL output, then report. If you truly " +
			"cannot run it here, report status \"failed\" and say plainly what stopped you — do not report " +
			"unverified work as done."
		pd, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: "m_" + newID(),
			Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
		})
		a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
		return loopContinue, "", true
	}
	// Prefer the answer the model already wrote as its message (it streamed live to the pane).
	// Only when the model put the answer in report.summary do we append it as the final
	// assistant message so the pane shows it.
	answer := strings.TrimSpace(rep.summary)
	if answer == "" {
		answer = lastText
	} else {
		paneText := answer
		if strings.TrimSpace(rep.details) != "" {
			paneText += "\n\n" + rep.details
		}
		a.appendPart(ctx, sid, tc.actor, "m_"+newID(), session.RoleAssistant, session.Part{
			ID: "p_" + newID(), Kind: session.PartText, Text: paneText,
		})
	}
	d, _ := json.Marshal(event.TurnFinishedData{Usage: u})
	a.appendFact(ctx, sid, event.TypeTurnFinished, tc.actor, d)
	return loopFinish, rep.result(answer), true
}

// publishContextUsage emits a live context meter for the UI (M6/context mgmt).
// outTokens is the turn's cumulative output so far, for the live ↓ readout (§8.1).
func (a *App) publishContextUsage(sid session.SessionID, actor event.Actor, modelID, sys string, msgs []session.Message, outTokens int) {
	window := a.contextWindow(modelID)
	tokens := a.contextTokens(sid, sys, msgs)
	pct := 0.0
	if window > 0 {
		pct = float64(tokens) / float64(window) * 100
	}
	d, _ := json.Marshal(event.ContextUsageData{Tokens: tokens, Window: window, Percent: pct, OutTokens: outTokens})
	a.publishTransient(sid, event.TypeContextUsage, actor, d)
}

// checkAutoOrchestration triggers auto-orchestration mode when context usage
// exceeds the configured threshold. Only fires once per session, only at depth 0.
// Returns true if it injected the orchestration directive this call, so the caller
// can re-read events and rebuild msgs to include the directive in the SAME turn.
func (a *App) checkAutoOrchestration(ctx context.Context, sid session.SessionID, depth int, modelID, sys string, msgs []session.Message) bool {
	if depth != 0 {
		return false // only top-level orchestrator
	}
	if a.cfg.Planner {
		// The pre-flight planner is the primary (calmer, framed-as-data) orchestration
		// mechanism. Stacking the reactive directive on top is redundant and its
		// alarming tone reads as a prompt injection — let the planner own this.
		return false
	}
	if a.cfg.AutoOrchestrate < 0 {
		return false // explicitly disabled
	}
	a.mu.Lock()
	if st, ok := a.stateIf(sid); ok && st.autoOrchestrate {
		a.mu.Unlock()
		return false // already triggered
	}
	a.mu.Unlock()

	window := a.contextWindow(modelID)
	if window == 0 {
		return false
	}
	tokens := a.contextTokens(sid, sys, msgs)
	ratio := float64(tokens) / float64(window)

	if ratio > a.cfg.AutoOrchestrate {
		a.mu.Lock()
		a.stateLocked(sid).autoOrchestrate = true
		a.mu.Unlock()

		a.injectOrchestrationDirective(ctx, sid, ratio)
		return true
	}
	return false
}

// injectOrchestrationDirective injects a system message forcing the agent into
// orchestration mode — decompose work and delegate to subagents.
func (a *App) injectOrchestrationDirective(ctx context.Context, sid session.SessionID, ratio float64) {
	text := fmt.Sprintf("magi runtime note (not user input): the context window is about %.0f%% full. "+
		"To keep things efficient on this larger task, prefer delegating the remaining INDEPENDENT pieces to "+
		"subagents via the task tool (in parallel where they don't depend on each other), then synthesize their "+
		"results, instead of doing everything inline. Skip this if the work isn't easily separable.", ratio*100)

	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "auto-orchestrate"}, pd)
}

// emitArtifact persists an artifact emitted by a tool/subagent (D11).
func (a *App) emitArtifact(ctx context.Context, sid session.SessionID, actor event.Actor, art artifact.Artifact) {
	d, _ := json.Marshal(event.ArtifactEmittedData{Artifact: art})
	a.appendFact(ctx, sid, event.TypeArtifactEmitted, actor, d)
}

func (a *App) appendPart(ctx context.Context, sid session.SessionID, actor event.Actor, msgID string, role session.Role, part session.Part) {
	d, _ := json.Marshal(event.PartAppendedData{MessageID: msgID, Role: role, Part: part})
	a.appendFact(ctx, sid, event.TypePartAppended, actor, d)
}

// appendReplyPart is appendPart for an inline interjection answer: it tags the part with
// InReplyTo (the answered message's origin MessageID) so the display layer can pair the
// answer with its question. replyTo=="" behaves exactly like appendPart.
func (a *App) appendReplyPart(ctx context.Context, sid session.SessionID, actor event.Actor, msgID, replyTo string, role session.Role, part session.Part) {
	d, _ := json.Marshal(event.PartAppendedData{MessageID: msgID, Role: role, Part: part, InReplyTo: replyTo})
	a.appendFact(ctx, sid, event.TypePartAppended, actor, d)
}

func (a *App) appendToolResult(ctx context.Context, sid session.SessionID, actor event.Actor, msgID, callID, content string, isErr bool) {
	c, _ := json.Marshal(content)
	a.appendPart(ctx, sid, actor, msgID, session.RoleTool, session.Part{
		ID:         "p_" + newID(),
		Kind:       session.PartToolResult,
		ToolResult: &session.ToolResult{CallID: callID, Content: c, IsError: isErr},
	})
}

func (a *App) emitError(ctx context.Context, sid session.SessionID, actor event.Actor, msg string) {
	// Every emitError site is a provider/stream failure; carry the machine code so
	// the headless contract ("error[<code>]: …" on stderr) holds for them too.
	d, _ := json.Marshal(event.ErrorData{Message: msg, Code: "provider"})
	a.appendFact(ctx, sid, event.TypeError, actor, d)
}

// allParallelSafe reports whether every tool call is read-only (no permission
// gate, not a subagent spawn), so the batch can run concurrently.
func (a *App) allParallelSafe(calls []*session.ToolCall) bool {
	for _, tc := range calls {
		// File modifiers must run sequentially regardless of the (user-configurable)
		// DangerTools set: the council change-capture and self-regression history read
		// each file's before/after around the edit, which is only race-free when writes
		// to the same file are serialized.
		if fileModifiers[tc.Name] || a.cfg.DangerTools[tc.Name] || tc.Name == "task" {
			return false
		}
	}
	return true
}
