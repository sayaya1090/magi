package app

import (
	"context"
	"encoding/json"
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
func (a *App) buildStepSystem(agent AgentSpec, workdir string, evs []event.Event) string {
	sys := a.systemFor(agent, workdir)
	// Language lock: weak models ignore a "reply in the user's language" rule buried in a long
	// prompt, so detect the user's script and put a short, forceful directive FIRST (primacy). Lock
	// to the genuine user's language, NOT the latest user-role message — council/hook/auto feedback
	// is injected as a user-role prompt (often English), which would let a weak model drift.
	if dir := langDirective(lastUserPromptText(evs)); dir != "" {
		sys = dir + "\n\n" + sys
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
	stopChecked      bool // stop hooks enforced at most once per turn
	nudgedEmpty      bool
	declareAsks      int    // how many times this turn was told to declare completion (declareAskCap)
	declared         bool   // the agent declared the task finished and the council accepted
	execNudged       bool   // authored-but-never-executed nudge fired at most once (exec-evidence)
	prevFinishText   string // the answer the council rejected last round
	prevFinishCalls  int    // guard.callCount() at that rejection (-1 = none yet)
	unverifiedReason string // non-empty when the turn finishes WITHOUT council approval
	recovered        bool   // stuck-recovery redecompose fired at most once (shared with the stall path)
	stepNudged       bool   // deliverable-check failure nudge injected at most once (MAGI_STEP_VERIFY)
	lastCheckEpoch   int    // mutation epoch at the last incremental step-check pass (skip read-only turns)
	lastCheckExec    int    // guard.execActivity() at that pass — also fire on a runtime action (server up, pkg installed)
	lastChecksVer    int    // checksVersion() at that pass — also fire when a re-plan derives new checks mid-run
	substRounds      int    // substitution-review correction rounds spent this turn (solo path)
	// substCritique carries the LAST substitution-review rejection into the next review round. The
	// agent is handed the critique to act on, but the council was re-convened knowing nothing about
	// it — so a member could not check whether its own objection had been met and was free to raise
	// a different one each round. Same amnesia the contract and plan re-rounds had.
	substCritique string
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
	// Tag the context ONCE so every request made under this run — the agent's stream, the council's
	// polls, every side call — is attributed to this session without its own plumbing.
	ctx = ctxWithUsageSID(ctx, sid)
	// Baseline for the turn's BILLED totals: the meter is cumulative for the process, so what this
	// turn cost is the delta across it (this session and everything dispatched beneath it).
	usageAtStart := a.UsageFor(sid)
	runStart := time.Now() // self-measured wall clock (budget line + council cost control)
	a.mu.Lock()
	a.stateLocked(sid).turnStart = runStart // what a check means by "before the step ran"
	a.mu.Unlock()
	agentActor := event.Actor{Kind: event.ActorAgent, ID: orDefault(agent.Name, "default")}
	lastText := ""
	guard := newRunGuard()
	guard.stallConverge = stallConvergeEnabled() // D18a: collapse the stalled-nudge re-arm when a redirect produced no forward motion
	ts := turnState{prevFinishCalls: -1}         // per-turn mutable bookkeeping (finish guards, council accounting, stuck-recovery); zeroed field-wise on reground
	tc := turnCtx{s: s, agent: agent, depth: depth, maxSteps: maxSteps, actor: agentActor, runStart: runStart, guard: guard}
	// The turn's scratch directory: captured command output, and the TMPDIR every command runs
	// under. Created HERE because the turn is its lifetime — a child inherits the pointer at spawn
	// and never removes it, or the first child to finish would take its siblings' output with it.
	if depth == 0 {
		if sc := newTurnScratch(); sc != nil {
			a.setScratch(sid, sc)
			defer func() {
				a.setScratch(sid, nil)
				sc.remove()
			}()
		}
	}
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
	// Fallback usage accumulation: the agent's OWN stream only. The reported numbers come from the
	// meter (turnUsage); these stand in when it saw nothing — a backend that reports no usage block,
	// or a test double that streams events directly.
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
		ts.prevFinishText = ""
		ts.prevFinishCalls = -1
		ts.stepNudged = false // a re-grounded task gets a fresh deliverable-check nudge budget
	}

	// No ceiling. A turn ends when the model stops calling tools, when the agent declares completion
	// and the council accepts it, when the caller's context is cancelled, or when whoever launched
	// magi stops waiting — the endings that were always real. The step cap was one more, and the
	// only one that ended a run on magi's own arithmetic rather than on something that happened.
	//
	// A workflow PHASE is the exception, because it declares its own budget as part of the
	// pipeline's shape (localize gets 14 steps, summarize gets 3), and a phase that ignores its
	// budget is a phase without a boundary. maxSteps<=0 means the caller set none.
	for step := 0; maxSteps <= 0 || step < maxSteps; step++ {
		if ctx.Err() != nil {
			return lastText, ctx.Err()
		}
		a.setStage(sid, stageExecute) // tag this iteration's events as execute (D15)
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
			ctrl := a.takeTurnControl(sid)
			// The drain empties every control field, so the declaration signal has to be caught
			// HERE or it is thrown away before the finish check ever sees it.
			if ctrl.finish {
				ts.declared = true
			}
			if tc := ctrl; tc.route != "" || tc.replan {
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
					if a.honorReplan(ctx, sid, tc.reason, &replanCount, &replanAtCalls, guard.callCount(), reground) {
						// Record the honored replan against the sterile-replan convergence counter:
						// if the completed-step high-water has not advanced across repeated replans,
						// handleStuckGuard lands the run UNVERIFIED rather than re-decomposing forever.
						guard.noteReplan(a.completedStepCount(sid))
					}
				}
			}
			// The second return said "reply to this interjection instead of parking"; there is no
			// park left to skip, so only the handled count is carried.
			handledUserPrompts, _ = a.detectInterjections(ctx, tc, evs, turnTask, handledUserPrompts)
		}

		// One model response for this step, with the two recoveries that belong to getting it:
		// a context too large to send, and a backend that goes silent (see generateStep).
		sr, gerr := a.generateStep(ctx, tc, agent, agentActor, guard, evs, step, cumOut)
		if gerr != nil {
			return lastText, gerr
		}
		res, evs := sr.res, sr.evs
		msgID, textPartID, reasonPartID := sr.msgID, sr.textPartID, sr.reasonPartID
		// Reasoning-only spin: the response was cancelled after streaming huge output with no tool
		// call. Discard it (it's garbage), nudge the agent to ACT, and move on — the step/stall
		// guards never see a response that never finishes, so this is the only place to break it.
		if res.reasoningSpun {
			a.emitToolProgress(sid, agentActor, "", agent.Name, "cancelled a reasoning-only spin — take a concrete action")
			_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"}, reasoningSpinNudge)
			continue
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
		}
		for _, tc := range toolCalls {
			a.appendPart(ctx, sid, agentActor, msgID, session.RoleAssistant, session.Part{
				ID: "p_" + newID(), Kind: session.PartToolCall, ToolCall: tc,
			})
		}

		// No tool calls → the turn wants to finish. Stop hooks enforce checks
		// (e.g. tests must pass); a failure pushes the agent to keep working.
		// The agent declared the task finished and the council accepted, so this turn is over — but
		// it ends the way every other turn ends. Returning from here directly would skip the finish
		// path itself: no turn.finished, no finalize stage, and a steer that landed during the
		// declaration left stranded instead of picked up as its own turn.
		if ts.declared || a.finishDeclared(sid) {
			ts.declared = true
			toolCalls = nil
		}
		if len(toolCalls) == 0 {
			// Turn-cumulative usage (§8.1): out/cost summed across steps, in = last.
			u := turnUsage(a, sid, usageAtStart, lastIn, cumOut, cumCost)
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

		// Incremental per-step check recording: the main agent (depth 0) does a mixed plan's SOLO
		// steps in its own turns — executeSteps skipped them, so completeThrough never fired for
		// them. Record any newly-passing step's checks here so the panel fills as the agent works,
		// instead of the terminal gate recording them all at once. Gated on the mutation epoch having
		// advanced since the last pass: a check can only newly-pass if this turn changed state, so a
		// read-only turn runs nothing. Recording only — the termination gate still owns finish/nudge.
		if depth == 0 {
			// Fire on a mutation OR an exercising run OR a change to the check SET itself: a check can
			// newly-pass not only when a file changed but when a RUNTIME action took effect (a server
			// now listening, a package now importable — those bump execActivity, not the mutation
			// epoch), and a mid-run re-plan can DERIVE a new check for work already done (that bumps
			// neither, only checksVer). An epoch-only trigger deferred all three to the terminal gate —
			// the "checklist items don't tick off as I work" batch.
			ep, ex := guard.mutationEpoch(), guard.execActivity()
			if ep != ts.lastCheckEpoch || ex != ts.lastCheckExec {
				ts.lastCheckEpoch, ts.lastCheckExec = ep, ex
			}
		}

		// Explicit output contract: a subagent that filed a report has delivered its final
		// result and its turn ends now — no more steps, no bash-echo looping. handleReport may
		// refuse an unverified "done" once (loopContinue) or finish the turn (loopFinish, with
		// The SAME accounting the other finish path uses. This built its own from the agent's stream
		// alone, so a turn that ended here published the last prompt where the bill was — measured on
		// a headless run that printed `tokens in 1721335` beside a turn.finished carrying `"in":48519`,
		// the two outputs agreeing and the inputs thirty-five fold apart. Two finish paths must not
		// have two accountings.
		u := turnUsage(a, sid, usageAtStart, lastIn, cumOut, cumCost)
		_ = u

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
	// Only a workflow phase reaches here: it spent the budget its own pipeline gave it. The work
	// stands; the phase simply stops, and the engine moves on to the next one.
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
		turnTask = lastUserPromptText(evs) // the prompt that drove this turn
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
	sid := tc.s.ID
	prompts := userPromptEntries(evs)
	if len(prompts) <= handledUserPrompts {
		return handledUserPrompts, false
	}
	// Handle EVERY user prompt that appeared since the last check, not just the newest: two
	// messages steered in during one long step would otherwise advance the counter past the
	// earlier one, dropping it silently.
	var newest, newestID string
	for _, it := range prompts[handledUserPrompts:] {
		if txt := strings.TrimSpace(it.Text); txt != "" && txt != strings.TrimSpace(turnTask) {
			a.markInterjectSeen(sid, it.MsgID)
			// Defer: queue it (masked from the live model context too) to run as its own turn.
			a.enqueueInterject(ctx, sid, it.MsgID, txt)
			newest, newestID = txt, it.MsgID
		}
	}
	if newest != "" {
		a.noteInterjection(sid, turnTask, newestID, newest)
	}
	return len(prompts), false
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
	sys := a.buildStepSystem(agent, s.Workdir, evs)

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
	vol := withNote(a.volatileContext(ctx, s, agent, evs, raw, step, tc.maxSteps, time.Since(tc.runStart)))

	// Context-aware auto-compaction (M6): if the assembled context exceeds the model's
	// window budget, summarize older turns and re-read. Measure against sys+vol so the
	// trigger still accounts for the volatile block (it's only used for sizing here).
	if a.maybeCompact(ctx, s, agent, agentActor, evs, raw, sys+"\n\n"+vol) {
		evs, _ = a.store.Read(ctx, sid, 0)
		raw = reconstruct(evs) // refresh after compaction
		vol = withNote(a.volatileContext(ctx, s, agent, evs, raw, step, tc.maxSteps, time.Since(tc.runStart)))
	}

	msgs := reconstruct(a.liveEvents(sid, evs))
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
		Tools:    a.toolSpecs(agent),
	}, evs
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

// emitArtifact persists an artifact emitted by a tool/subagent (D11).
func (a *App) emitArtifact(ctx context.Context, sid session.SessionID, actor event.Actor, art artifact.Artifact) {
	d, _ := json.Marshal(event.ArtifactEmittedData{Artifact: art})
	a.appendFact(ctx, sid, event.TypeArtifactEmitted, actor, d)
}

// appendPart records one part of a message. A part that cannot be marshalled is recorded as the
// FAILURE, never as nothing: `d, _ := json.Marshal(…)` leaves d nil on error, and appending that
// wrote an event with a null payload — which reconstructs to no part at all. Measured: two `read`
// calls whose oversized content left invalid JSON (see capToolResult) produced exactly that, so the
// agent saw two tool calls with no answer of any kind and moved on without the files it asked for.
//
// A tool result keeps its call id in the fallback, because a result that cannot be paired with its
// call is not much better than none: the agent has to be able to see WHICH call this answers.
func (a *App) appendPart(ctx context.Context, sid session.SessionID, actor event.Actor, msgID string, role session.Role, part session.Part) {
	d, err := json.Marshal(event.PartAppendedData{MessageID: msgID, Role: role, Part: part})
	if err != nil {
		d = unrecordablePart(msgID, role, part, err)
	}
	a.appendFact(ctx, sid, event.TypePartAppended, actor, d)
}

// unrecordablePart builds the payload that stands in for a part the store could not carry. It says
// what happened in the part's own kind, so the agent reads it where it was looking for the answer.
func unrecordablePart(msgID string, role session.Role, part session.Part, cause error) []byte {
	msg := "this result could not be recorded (" + cause.Error() + "). The call ran; its output is " +
		"not readable here. Re-run it in a narrower form — a smaller range, a filter, fewer results."
	sub := session.Part{ID: part.ID, Kind: session.PartText, Text: msg}
	if part.Kind == session.PartToolResult && part.ToolResult != nil {
		c, _ := json.Marshal(msg)
		sub = session.Part{ID: part.ID, Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: part.ToolResult.CallID, Content: c, IsError: true,
		}}
	}
	d, err := json.Marshal(event.PartAppendedData{MessageID: msgID, Role: role, Part: sub})
	if err != nil { // the fallback holds only strings this package built; unreachable in practice
		return []byte(`{"messageId":"","role":"tool","part":{"kind":"text","text":"a part could not be recorded"}}`)
	}
	return d
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

// turnUsage is what a finished turn reports: the BILL, measured as the meter's delta across the turn
// for this session and everything dispatched beneath it.
//
// The fallback matters. The meter only sees what a backend actually reports, so a provider that
// sends no usage block leaves it empty — and reporting zero where the old accounting had a number
// would be a regression dressed as a fix. When the meter saw nothing, the agent's own stream totals
// stand in, which is exactly what was reported before.
func turnUsage(a *App, sid session.SessionID, start event.Usage, lastIn, cumOut int, cumCost float64) event.Usage {
	now := a.UsageFor(sid)
	u := event.Usage{
		In:   now.In - start.In,
		Out:  now.Out - start.Out,
		Cost: now.Cost - start.Cost,
	}
	if u.In <= 0 && u.Out <= 0 {
		return event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
	}
	if u.Cost <= 0 {
		u.Cost = cumCost
	}
	return u
}
