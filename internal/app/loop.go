package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/council"
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

// streamStep is the outcome of consuming one model-response stream: the finalized
// assistant text/reasoning, the tool calls it requested, and the usage report.
type streamStep struct {
	text         string
	reasoning    string
	toolCalls    []*session.ToolCall
	usage        *event.Usage
	textConsumed bool // the text was a prompt-fallback tool call, not a real answer
}

// consumeStream drains one provider stream, publishing text/reasoning deltas as
// transient events and recording the real prompt-token count for the meter. A
// non-nil error means the provider reported one (already emitted to the bus) and
// the turn must unwind.
// streamDiag enables opt-in stderr stream diagnostics (MAGI_STREAM_DIAG), mirroring
// the adapter-side flag so pre-finish stalls and post-finish drains can be traced
// together in one run without affecting normal operation.
var streamDiag = os.Getenv("MAGI_STREAM_DIAG") != ""

func (a *App) consumeStream(ctx context.Context, sid session.SessionID, agentActor event.Actor, stream <-chan port.ProviderEvent, msgID, textPartID, reasonPartID string) (streamStep, error) {
	var text, reasoning strings.Builder
	var res streamStep
	streamErr := false
	// Opt-in diagnostics (MAGI_STREAM_DIAG): distinguish a pre-finish stall (model
	// slow / no bytes) from a post-finish drain delay (backend withholding [DONE]).
	// idleC stays nil when disabled, so the select degenerates to a plain range.
	var (
		idleC    <-chan time.Time
		last     = time.Now()
		finished bool
		finishAt time.Time
	)
	if streamDiag {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		idleC = t.C
	}
loop:
	for {
		var ev port.ProviderEvent
		select {
		case e, ok := <-stream:
			if !ok {
				break loop
			}
			ev = e
			last = time.Now()
		case now := <-idleC:
			if gap := now.Sub(last); gap >= 20*time.Second {
				fmt.Fprintf(os.Stderr, "magi: stream idle %s (finished=%v) sid=%s\n", gap.Round(time.Second), finished, sid)
				last = now // re-arm; report each sustained gap once
			}
			continue
		}
		switch ev.Type {
		case port.ProviderReasoning:
			reasoning.WriteString(ev.Text)
			d, _ := json.Marshal(event.PartDeltaData{MessageID: msgID, PartID: reasonPartID, Kind: session.PartReasoning, Text: ev.Text})
			a.publishTransient(sid, event.TypePartDelta, agentActor, d)
		case port.ProviderText:
			text.WriteString(ev.Text)
			d, _ := json.Marshal(event.PartDeltaData{MessageID: msgID, PartID: textPartID, Kind: session.PartText, Text: ev.Text})
			a.publishTransient(sid, event.TypePartDelta, agentActor, d)
		case port.ProviderToolCall:
			res.toolCalls = append(res.toolCalls, ev.ToolCall)
			if ev.FromText {
				res.textConsumed = true // text was actually a tool call (fallback)
			}
		case port.ProviderUsage:
			res.usage = ev.Usage
			if ev.Usage != nil && ev.Usage.In > 0 {
				a.setPromptTokens(sid, ev.Usage.In) // real context size for meter/compaction
			}
		case port.ProviderFinish:
			finished = true
			finishAt = time.Now()
		case port.ProviderError:
			a.emitError(ctx, sid, agentActor, ev.Err.Error())
			streamErr = true
		}
	}
	if streamDiag && finished {
		if d := time.Since(finishAt); d > 2*time.Second {
			fmt.Fprintf(os.Stderr, "magi: stream drained %s after finish sid=%s\n", d.Round(time.Millisecond), sid)
		}
	}
	res.text = text.String()
	res.reasoning = reasoning.String()
	if streamErr {
		return res, fmt.Errorf("provider error")
	}
	return res, nil
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
	prevFinishText   string      // the answer the council rejected last round
	prevFinishCalls  int         // guard.callCount() at that rejection (-1 = none yet)
	unverifiedReason string      // non-empty when the turn finishes WITHOUT council approval
	recovered        bool        // stuck-recovery redecompose fired at most once (shared with the stall path)
	council          councilTurn // consensus gate rounds/feedback/spent/deadlock (D14)
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
	turnTask := ""                               // the user instruction THIS turn answers, snapshotted at step 0. A
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
	if a.planEligible(agent, depth) {
		planned, _ := a.maybePlanPreflight(ctx, s, depth, maxSteps, "")
		if planned {
			usedTools = true // planner did real work — seed the termination council
		}
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
			// turnTask is the prompt that SEEDED this turn — the first genuine user prompt
			// not already answered by a previous turn — NOT merely the latest. User prompts
			// that piled up after the seed but before execution began (e.g. while a synchronous
			// planning/explorer phase held the loop) are interjections: queue them so they run
			// as their own turns and never become what the council judges against. (Top level
			// only; a subagent session has no ActorUser prompts so seedPromptIdx returns -1.)
			entries := userPromptEntries(evs)
			if depth == 0 && !a.cfg.Workflow {
				if seed := seedPromptIdx(evs); seed >= 0 && seed < len(entries) {
					turnTask = entries[seed].Text
					for _, it := range entries[seed+1:] {
						if txt := strings.TrimSpace(it.Text); txt != "" && txt != strings.TrimSpace(turnTask) {
							a.markInterjectSeen(sid, it.MsgID)
							a.enqueueInterject(sid, it.MsgID, txt)
						}
					}
				} else {
					turnTask = lastUserPromptText(evs)
				}
			} else {
				turnTask = lastUserPromptText(evs) // the prompt that drove this turn
			}
			handledUserPrompts = len(entries) // baseline; a later rise is a mid-turn interjection
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
			// Detect a mid-turn user interjection (a new genuine user prompt appeared since we
			// last absorbed one). Top level only — subagents aren't steered by the user. Every
			// interjection is masked from turnTask/council derivation (markInterjectSeen) so it
			// can't swap what the council judges against. How the orchestrator handles it depends
			// on what it is doing:
			//   - idle-parked waiting on its own explorers (awaitingExplorers): it has no work to
			//     interleave, so a soft "MAY answer" directive starves the reply (observed: asides
			//     dropped for the whole delegated task). Run handleAside — a focused tool-capable
			//     turn that replies to chitchat OR signals a steer (route_interjection to redirect/
			//     append/queue, cancel_dispatch to stop now-irrelevant subagents, ask_user to
			//     clarify). If it acts on the work we break the park so the next normal step
			//     applies the route and re-dispatches with the full toolset.
			//   - ordinary background delegation (dispatching but not parked): the orchestrator
			//     keeps running its own turns, so let the next working turn answer via the soft
			//     directive (answering in a separate turn here would double-reply, and the aside is
			//     left visible so the working turn can route it).
			//   - otherwise idle: queue it to run as its own turn and tell the agent it is deferred
			//     so it stops oscillating (it may still call route_interjection to redirect/append).
			if depth == 0 && !a.cfg.Workflow {
				prompts := userPromptEntries(evs)
				if len(prompts) > handledUserPrompts {
					dispatching := a.bgOutstanding(sid) > 0
					idleWaiting := dispatching && a.awaitingExplorers(sid) // parked with no work to interleave
					// Handle EVERY user prompt that appeared since the last check, not just the
					// newest: two messages steered in during one long step would otherwise advance
					// the counter past the earlier one, dropping it silently.
					var newest, newestID string
					for _, it := range prompts[handledUserPrompts:] {
						if txt := strings.TrimSpace(it.Text); txt != "" && txt != strings.TrimSpace(turnTask) {
							a.markInterjectSeen(sid, it.MsgID)
							switch {
							case idleWaiting:
								// Enqueue-first so route_interjection (which requires a pending
								// interjection) can fire, then run the focused handler. It consumes a
								// resolved chitchat reply / bare cancel itself and leaves a routed
								// redirect/append queued for the turnControl drain to apply.
								a.enqueueInterject(sid, it.MsgID, txt)
								if a.handleAside(ctx, agent, s, depth, turnTask, it.MsgID, txt) {
									answerInterjectNow = true // break the park so the route/cancel takes effect next step
								}
							case !dispatching:
								// Defer: queue it (masked from the live model context too) to run as
								// its own turn.
								a.enqueueInterject(sid, it.MsgID, txt)
							}
							// Ordinary dispatch (dispatching && !idleWaiting): left visible for the
							// interleaving working turn to answer via the soft directive below.
							newest, newestID = txt, it.MsgID
						}
					}
					handledUserPrompts = len(prompts)
					// idle-park is fully owned by handleAside above. The directive is only for the
					// other cases (non-dispatch queue notice, ordinary-dispatch soft answer).
					if newest != "" && !idleWaiting {
						a.injectInterjectDirective(ctx, sid, turnTask, newestID, newest, dispatching)
						if dispatching {
							// The directive we just appended is the last event, so lastIsUserSteer/
							// needsOrchestratorTurn would read false and the early park below would
							// re-swallow it; skip the park this iteration so the model runs and replies.
							answerInterjectNow = true
						}
					}
				}
			}
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

		// Durable project memory (AGENTS.md) is part of the system prompt and is never
		// compacted away. The system prompt is assembled byte-stable within a turn (so
		// the backend's prefix/KV cache survives across steps); per-step-volatile
		// context (current plan/TODOs, shared experience, retrieved RAG) is injected
		// separately as an ephemeral trailing message below, NOT into sys.
		isSub := s.Parent != ""
		sys := a.buildStepSystem(agent, s.Workdir, isSub, evs)

		// Per-step-volatile context (current plan, shared experience, retrieved RAG): built
		// here but injected as an ephemeral trailing message, NOT into `sys`. `sys` (above) is
		// now byte-stable within a turn, so the backend's prefix cache is reused across steps;
		// only this small block at the tail is re-processed each step.
		vol := a.volatileContext(ctx, s, agent, isSub, evs, step, maxSteps, time.Since(runStart))

		// Context-aware auto-compaction (M6): if the assembled context exceeds the model's
		// window budget, summarize older turns and re-read. Measure against sys+vol so the
		// trigger still accounts for the volatile block (it's only used for sizing here).
		if a.maybeCompact(ctx, s, agent, agentActor, evs, sys+"\n\n"+vol) {
			evs, _ = a.store.Read(ctx, sid, 0)
			vol = a.volatileContext(ctx, s, agent, isSub, evs, step, maxSteps, time.Since(runStart)) // refresh after compaction
		}

		msgs := reconstruct(a.liveEvents(sid, evs))
		// If auto-orchestration fires, it injects a directive as a new event; re-read
		// and rebuild msgs so the directive reaches the model in THIS turn, not the next.
		if a.checkAutoOrchestration(ctx, sid, depth, s.Model.Model, sys, msgs) {
			if evs2, err := a.store.Read(ctx, sid, 0); err == nil {
				evs = evs2
				msgs = reconstruct(a.liveEvents(sid, evs))
				vol = a.volatileContext(ctx, s, agent, isSub, evs, step, maxSteps, time.Since(runStart))
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

		req := port.ChatRequest{
			Model:    s.Model.Model,
			System:   sys,
			Messages: msgs,
			Tools:    a.toolSpecs(agent, isSub, depth),
		}

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
			switch a.finishTurn(ctx, s, agent, depth, maxSteps, step, turnTask, lastText, evs, agentActor, usedTools, handledUserPrompts, runStart, u, guard, &ts) {
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

		// Explicit output contract: a subagent that filed a report has delivered its
		// final result and its turn ends now — no more steps, no bash-echo looping.
		if rep := a.takeReport(sid); rep != nil {
			// A subagent finishing via report short-circuits the pre-finish gates above:
			// self-verify, the structural fabrication check, and the council are all top-level
			// only, and this return fires before the len(toolCalls)==0 finish branch ever runs.
			// So the delegated path needs its own check. A "done" report whose deliverable was
			// changed but never exercised (unverifiedDeliverable, keyed off this subagent's own
			// tool log) is not done — the language-agnostic replacement for the report tool's
			// former English confession-phrase scan. Gated on the subagent actually HAVING a way
			// to run its work (bash): a read/write-only agent cannot execute anything, so blocking
			// it for "not running it" would be a false positive — that case defers to the parent's
			// review-gate tester, which runs the merged deliverable for real. Refuse ONCE.
			if rep.status == "done" && !reportRefused && agent.allows("bash") && guard.unverifiedDeliverable() {
				reportRefused = true
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
				continue
			}
			// Prefer the answer the model already wrote as its message (it streamed
			// live to the pane). Only when the model put the answer in report.summary
			// do we append it as the final assistant message so the pane shows it.
			answer := strings.TrimSpace(rep.summary)
			if answer == "" {
				answer = lastText
			} else {
				paneText := answer
				if strings.TrimSpace(rep.details) != "" {
					paneText += "\n\n" + rep.details
				}
				a.appendPart(ctx, sid, agentActor, "m_"+newID(), session.RoleAssistant, session.Part{
					ID: "p_" + newID(), Kind: session.PartText, Text: paneText,
				})
			}
			u := event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
			d, _ := json.Marshal(event.TurnFinishedData{Usage: u})
			a.appendFact(ctx, sid, event.TypeTurnFinished, agentActor, d)
			finished = true // report filed → turn delivered its result
			return rep.result(answer), nil
		}

		// Corrective re-grounding: before the force-stop, give a thrashing agent ONE nudge
		// to re-read the original task and change approach — a stuck weak model often just
		// needs redirecting, and this is far cheaper than burning the rest of the budget.
		if kind := guard.shouldNudge(); kind != "" {
			// turnTask is empty for a subagent run (its seed is authored by ActorAgent, not
			// ActorUser), so fall back to the latest user-role message — the subagent's task —
			// mirroring the council gate's defensive fallback. Otherwise the re-grounding
			// would no-op exactly where weak models thrash most (narrow tool-driven subtasks).
			task := strings.TrimSpace(turnTask)
			if task == "" {
				task = strings.TrimSpace(lastUserText(reconstruct(a.taskEvents(sid, evs))))
			}
			msg := "You've repeated the same no-progress action several times and are getting blocked. " +
				"Stop and change approach: try a different tool or a smaller step, or inspect WHY the last " +
				"attempts failed (read the error, check paths/state) before retrying. Re-read the original task:\n" +
				clipSpec(task, 1500)
			if kind == "spin" {
				msg = "You've run a no-op command (echo/printf/true) several times in a row — a \"done\" banner is " +
					"not a step and does not finish the task. If the work is genuinely COMPLETE: end your turn now by " +
					"replying with NO tool call at all — that is what triggers verification and completion; do NOT run " +
					"another command. If it is NOT complete: stop announcing success and take a real action — run the " +
					"actual program/test against the deliverable, or fix what's failing. Re-read the original task:\n" +
					clipSpec(task, 1500)
			}
			if kind == "stalled" {
				msg = "You've run many steps without changing anything or making concrete progress — you may be " +
					"re-running checks or restating the same conclusion instead of advancing the task. Stop and take a " +
					"DIFFERENT concrete action toward the deliverable; if something is blocking you, state exactly what " +
					"it is and why before continuing. Guess only when necessary: if a value is unknown but discoverable, " +
					"run the tool or command that determines it (compute, parse, crack, query, or read the real state) " +
					"rather than trying values blindly. Re-read the original task:\n" +
					clipSpec(task, 1500)
			}
			pd, _ := json.Marshal(event.PromptSubmittedData{
				MessageID: "m_" + newID(),
				Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
			})
			a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
		}

		// Loop guard: stop rather than burning the full step budget, on hard repeats or
		// on a stall the agent kept ignoring after every nudge (varied-but-unproductive
		// calls never trip the repeat count). HOW it stops depends on whether the run
		// produced a deliverable: a run that already wrote real output (epoch > 0) and is
		// only spinning on confirmation is effectively DONE — finish it cleanly (exit 0)
		// with its last text, rather than flagging an agent-level error that misreports a
		// completed task as a failure (the false NonZeroAgentExitCodeError seen on tasks
		// that actually passed). A run that produced NOTHING is genuine thrash — keep the
		// error abort so the failure is visible.
		if kind := guard.stuck(); kind != "" {
			// Last-resort structural recovery: a stall the agent kept ignoring after every
			// nudge means it is genuinely stuck, not just slow. Before force-stopping, hand the
			// remaining work to a fresh child that re-plans from scratch (redecomposeStuck),
			// ONCE, for a plan-eligible (write-capable, below the depth cap) agent. planEligible
			// (not a depth==0 guard) is intentional: a stuck solo SUBAGENT below the plan-depth
			// cap can recover the same way; the child is depth+1 so its own planEligible bounds
			// further recursion. On success, reset the stall window and loop so the parent
			// integrates and verifies the child's result; on failure fall through to the stop.
			// recovered is set only on a successful spawn, so a transient child failure does not
			// permanently disable the (still fire-at-most-once) hook.
			if kind == "stall" && !ts.recovered && a.planEligible(agent, depth) {
				task := strings.TrimSpace(turnTask)
				if task == "" {
					task = strings.TrimSpace(lastUserText(reconstruct(a.taskEvents(sid, evs))))
				}
				if a.redecomposeStuck(ctx, s, agent, task,
					"repeated no-progress: the previous attempt could not advance the task", depth) {
					ts.recovered = true
					guard.resetStall()
					continue
				}
			}
			if guard.mutationEpoch() > 0 {
				// Delivered-but-spinning: finish cleanly (TurnFinished → exit 0) with the
				// work as-is. The repeated no-progress steps already stand in the transcript,
				// so no error event is needed to explain the stop.
				a.setStage(sid, stageFinalize)
				u := event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
				fd, _ := json.Marshal(event.TurnFinishedData{Usage: u})
				a.appendFact(ctx, sid, event.TypeTurnFinished, agentActor, fd)
				finished = true
				return lastText, nil
			}
			msg, code := "stopped: the agent repeated the same action without progress (loop guard)", "loop_guard"
			if kind == "stall" {
				msg, code = "stopped: no real progress after repeated redirection (stall guard)", "stall_guard"
			}
			if kind == "spin" {
				msg, code = "stopped: repeated completion-banner spin without ending the turn (spin guard)", "spin_guard"
			}
			d, _ := json.Marshal(event.ErrorData{Message: msg, Code: code})
			a.appendFact(ctx, sid, event.TypeError, event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
			return lastText, nil
		}
	}

	// Max steps reached: stop gracefully.
	d, _ := json.Marshal(event.ErrorData{Message: "max steps reached", Code: "max_steps"})
	a.appendFact(ctx, sid, event.TypeError, event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
	return lastText, nil
}

// finishTurn runs the no-tool-call finish path for a single step: stop-hook enforcement,
// the empty-subagent nudge, the top-level background-subagent wait, the consensus council
// termination gate (with its idle-resubmission short-circuit and deadlock redecompose), and
// — when the turn truly ends — the late-steer sweep plus the turn.finished record. It mutates
// ts (the once-per-turn guards, council accounting, stuck-recovery flag, and UNVERIFIED reason)
// and returns a loopAction telling the step loop whether to keep looping, re-enter without
// spending a step, finish the turn, or unwind a cancellation. Extracted verbatim from runLoop's
// step loop — behavior is unchanged; the caller owns step/lastText and acts on the returned action.
func (a *App) finishTurn(ctx context.Context, s session.Session, agent AgentSpec, depth, maxSteps, step int, turnTask, lastText string, evs []event.Event, agentActor event.Actor, usedTools bool, handledUserPrompts int, runStart time.Time, u event.Usage, guard *runGuard, ts *turnState) loopAction {
	sid := s.ID
	isSub := s.Parent != ""
	if !ts.stopChecked {
		if fail := a.runStopHooks(ctx, s.Workdir); fail != "" {
			ts.stopChecked = true // enforce once per turn to avoid an infinite loop
			pd, _ := json.Marshal(event.PromptSubmittedData{
				MessageID: "m_" + newID(),
				Parts:     []session.Part{{Kind: session.PartText, Text: "A required check failed before finishing — fix it, then continue:\n" + fail}},
			})
			a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "hook"}, pd)
			return loopContinue
		}
	}
	// A turn must deliver a real result before finishing. Reaching this branch
	// means it produced no tool call this step.
	//   - Subagent: if report is available it has NOT filed one (report terminates
	//     the run earlier), so nudge it to call report with actual findings; when
	//     report is unavailable, only an EMPTY result warrants the nudge.
	//   - Top level: a turn that ran NO tool all turn AND ends with empty text —
	//     a reasoning-only stop, common with harmony-format weak models that emit
	//     only their analysis channel and stop — delivered literally nothing. The
	//     council gate can't catch it (it requires usedTools), so it would finish
	//     silently as a confident done with no deliverable and no UNVERIFIED flag.
	//     Nudge it once to actually produce a result instead of finishing empty.
	// Fires once per turn either way, so a still-empty retry then finishes normally.
	if !ts.nudgedEmpty {
		_, hasReport := a.tools.Get("report")
		reportAvail := isSub && hasReport && agent.allows("report")
		emptyResult := strings.TrimSpace(lastText) == ""
		if (isSub && (reportAvail || emptyResult)) || (!isSub && !usedTools && emptyResult) {
			ts.nudgedEmpty = true
			msg := "You are ending your turn without delivering a result. Call the 'report' tool NOW with " +
				"your actual findings/answer and a status (done/blocked/failed). Do not stop with a partial " +
				"thought; if the task isn't finished, continue it first."
			if !reportAvail {
				msg = "You ended without giving a result. Write your findings/answer for the task now as your message."
			}
			pd, _ := json.Marshal(event.PromptSubmittedData{
				MessageID: "m_" + newID(),
				Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
			})
			a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "orchestrator"}, pd)
			return loopContinue
		}
	}
	// Sidecar model (async): the orchestrator stays alive (UI-thread style)
	// while background subagents run, but it is re-invoked ONLY when there is
	// something for it to act on — all subagents done (synthesize), a real
	// user steer, or a subagent asking (escalation). It is NOT woken for each
	// individual subagent result (those accumulate silently), which is what
	// kept weak models fabricating results and re-dispatching per completion.
	// Waiting does not consume the step budget. Top-level only — subagents are
	// not user-steerable.
	if depth == 0 {
		for a.bgOutstanding(sid) > 0 && ctx.Err() == nil && !a.needsOrchestratorTurn(ctx, sid) {
			select {
			case <-a.bgWaitChan(sid):
			case <-ctx.Done():
			}
		}
		if ctx.Err() == nil && a.needsOrchestratorTurn(ctx, sid) {
			// Mark current results consumed so we don't re-wake for them again
			// (multi-wave delegation re-arms this when new results are injected).
			a.bgConsume(sid)
			return loopRetryStep
		}
		// Cancelled while parked in the bg-wait: return the cancellation like
		// every other interrupt site, rather than falling through to the council/
		// finalize path (which would emit a second turn.finished and report the
		// cancelled turn as a success).
		if ctx.Err() != nil {
			return loopAbort
		}
	}
	// Consensus council termination gate (D14): top level only, not in
	// workflow mode, and only for turns that did real work — a purely
	// conversational reply (no tool use, e.g. a greeting) has nothing to
	// verify, so gating it just churns and can derail a weak model.
	if depth == 0 && a.cfg.Council != nil && !a.cfg.Workflow && usedTools {
		// Structural fabrication evidence for the council: if the agent changed a
		// deliverable this turn but ran no command exercising the current version, that is a
		// hard, language-agnostic fact a text-only vote can't wave through. Replaces the old
		// English-only confession-phrase scan.
		fab := ""
		if guard.unverifiedDeliverable() {
			fab = "the agent changed a deliverable this turn but ran no command exercising the current version — it is unverified by execution"
		}
		// Idle resubmission short-circuit: the council rejected this answer,
		// and the agent came back having run NO tool and changed (almost)
		// nothing — re-deliberating the same evidence can only burn a round
		// and print the same answer twice. Finish instead, marked plainly.
		if ts.prevFinishCalls >= 0 && guard.callCount() == ts.prevFinishCalls && normEq(lastText, ts.prevFinishText) {
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: ts.council.rounds + 1, Decision: string(council.Done),
				Note: "answer resubmitted unchanged after council feedback — finishing without re-deliberation; treat as UNVERIFIED",
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
			ts.unverifiedReason = "the same answer was resubmitted unchanged after council feedback, without re-deliberation"
		} else {
			keepWorking, unv := a.runCouncilGate(ctx, s, agent, councilInput{
				turnTask:    turnTask,
				lastText:    lastText,
				changes:     buildCouncilChanges(guard.changeSet()),
				fabrication: fab,
				stepsLeft:   maxSteps - step,
				turnElapsed: time.Since(runStart),
			}, &ts.council)
			if keepWorking {
				ts.prevFinishText, ts.prevFinishCalls = lastText, guard.callCount()
				return loopContinue
			}
			// The gate is letting the turn finish. A non-empty reason means it did so
			// WITHOUT approving (deadlock/cost-cap/no-feedback/unavailable) — carry that
			// into turn.finished below so the UI shows UNVERIFIED, not a confident done.
			ts.unverifiedReason = unv
			if !ts.recovered && ts.council.deadlocked &&
				strings.TrimSpace(ts.council.feedback) != "" && a.planEligible(agent, depth) {
				// Council deadlock: the members used every round and never approved, still holding
				// an unmet concern — the gate flags this explicitly (ts.council.deadlocked), so a DONE
				// vote that merely landed on the last allowed round does NOT reach here. The agent
				// is stuck against a wall it could not clear on its own; before finishing UNVERIFIED,
				// hand the task plus that exact concern to a fresh child that re-plans and breaks it
				// down (ADaPT failure-recursion, solo-stuck branch). Fires once; on success, reset
				// the stall window and loop so the parent integrates and verifies the child's work.
				task := strings.TrimSpace(turnTask)
				if task == "" {
					task = strings.TrimSpace(lastUserText(reconstruct(a.taskEvents(sid, evs))))
				}
				if a.redecomposeStuck(ctx, s, agent, task, ts.council.feedback, depth) {
					ts.recovered = true
					guard.resetStall()
					return loopContinue
				}
			}
		}
	}
	// A user steer can land AFTER this step's top-of-loop interjection scan but
	// before the turn commits here: during the final (no-tool) step's model stream,
	// or during a council deliberation that then voted done. It was never enqueued
	// (only the top-of-loop scan enqueues), and the finish path persists the
	// assistant's text after it, so the run goroutine's last-message-role safety net
	// (hasUnansweredUserPrompt) is fooled by the trailing assistant message too — the
	// steer would be silently lost, not even queued. Re-read the store one last time
	// and enqueue any prompt that appeared past the baseline so it drains as its own
	// turn. Top level only — subagents are not user-steerable.
	if depth == 0 && !a.cfg.Workflow {
		a.enqueueLateInterjections(ctx, sid, handledUserPrompts, turnTask)
	}
	a.setStage(sid, stageFinalize) // turn is ending (D15)
	// A finish the council never approved (deadlock/cost-cap/resubmit) lands as
	// UNVERIFIED so the UI stops painting an abandoned task as a confident done.
	d, _ := json.Marshal(event.TurnFinishedData{Usage: u, Unverified: ts.unverifiedReason != "", Reason: ts.unverifiedReason})
	a.appendFact(ctx, sid, event.TypeTurnFinished, agentActor, d)
	return loopFinish
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
