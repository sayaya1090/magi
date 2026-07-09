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
	stopChecked := false   // Stop hooks enforced at most once per run
	nudgedEmpty := false   // subagent empty-result nudge fired at most once
	reportRefused := false // a subagent's unverified "done" report was refused once this run
	recovered := false     // stuck-recovery redecompose (stall/council deadlock → depth+1 child) fired at most once per run
	guard := newRunGuard()
	guard.stallConverge = stallConvergeEnabled() // D18a: collapse the stalled-nudge re-arm when a redirect produced no forward motion
	councilRounds := 0                           // consensus termination gate rounds this turn (D14)
	lastCouncilFeedback := ""                    // last round's feedback (no-progress detection)
	prevFinishText := ""                         // the answer the council rejected last round
	prevFinishCalls := -1                        // guard.callCount() at that rejection (-1 = none yet)
	councilSpent := time.Duration(0)             // self-measured wall-clock consumed by deliberations
	councilDeadlock := false                     // set by the gate iff its finish was a genuine round-cap deadlock (never approved)
	unverifiedReason := ""                       // non-empty when the turn finishes WITHOUT council approval (deadlock/cost-cap/resubmit); propagated to turn.finished so the UI marks it UNVERIFIED, not a clean done
	turnTask := ""                               // the user instruction THIS turn answers, snapshotted at step 0. A
	// steer that lands mid-turn is QUEUED by default (runs as its own follow-up turn), so
	// it can't silently hijack what the council judges against — unless the agent explicitly
	// routes it (route_interjection redirect/append re-snapshots turnTask here via reground).
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
	// pre-flight planner for a fresh decomposition.
	reground := func(rebuildPlan bool) {
		guard.resetStall()
		councilRounds = 0
		lastCouncilFeedback = ""
		prevFinishText = ""
		prevFinishCalls = -1
		councilDeadlock = false
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
				// redirect re-anchors turnTask on the interjection; append folds it in. Either
				// way the interjection is absorbed now, so don't also re-surface it as its own
				// turn, and reground so the stall/council accounting tracks the adopted task.
				// "queue" (or empty) leaves turnTask on the current task — the interjection stays
				// queued and runs as its own turn after this one ends.
				if it := strings.TrimSpace(lastUserPromptText(evs)); it != "" {
					if nt, changed := applyRoute(tc.route, turnTask, it); changed {
						turnTask = nt
						a.consumeInterject(sid, it)
						reground(true)
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
					var newest string
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
								if a.handleAside(ctx, agent, s, depth, turnTask, txt) {
									answerInterjectNow = true // break the park so the route/cancel takes effect next step
								}
							case !dispatching:
								// Defer: queue it (masked from the live model context too) to run as
								// its own turn.
								a.enqueueInterject(sid, it.MsgID, txt)
							}
							// Ordinary dispatch (dispatching && !idleWaiting): left visible for the
							// interleaving working turn to answer via the soft directive below.
							newest = txt
						}
					}
					handledUserPrompts = len(prompts)
					// idle-park is fully owned by handleAside above. The directive is only for the
					// other cases (non-dispatch queue notice, ordinary-dispatch soft answer).
					if newest != "" && !idleWaiting {
						a.injectInterjectDirective(ctx, sid, turnTask, newest, dispatching)
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
			if queued := a.pendingInterjectTexts(sid); len(queued) > 0 {
				for _, txt := range queued {
					if a.handleAside(ctx, agent, s, depth, turnTask, txt) {
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
			if !stopChecked {
				if fail := a.runStopHooks(ctx, s.Workdir); fail != "" {
					stopChecked = true // enforce once per turn to avoid an infinite loop
					pd, _ := json.Marshal(event.PromptSubmittedData{
						MessageID: "m_" + newID(),
						Parts:     []session.Part{{Kind: session.PartText, Text: "A required check failed before finishing — fix it, then continue:\n" + fail}},
					})
					a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "hook"}, pd)
					continue
				}
			}
			// A subagent must deliver a real result before finishing. Reaching this
			// branch means it produced no tool call — and if report is available, it
			// has NOT filed one (report terminates the run earlier). One nudge forces
			// it to call report with actual findings instead of returning whatever
			// stray text (often a mid-thought fragment) happened to be last. When
			// report is unavailable, only an EMPTY result warrants the nudge.
			if isSub && !nudgedEmpty {
				_, hasReport := a.tools.Get("report")
				reportAvail := hasReport && agent.allows("report")
				if reportAvail || strings.TrimSpace(lastText) == "" {
					nudgedEmpty = true
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
					continue
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
					step--
					continue
				}
				// Cancelled while parked in the bg-wait: return the cancellation like
				// every other interrupt site, rather than falling through to the council/
				// finalize path (which would emit a second turn.finished and report the
				// cancelled turn as a success).
				if ctx.Err() != nil {
					return lastText, ctx.Err()
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
				if prevFinishCalls >= 0 && guard.callCount() == prevFinishCalls && normEq(lastText, prevFinishText) {
					dd, _ := json.Marshal(event.CouncilDecidedData{
						Round: councilRounds + 1, Decision: string(council.Done),
						Note: "answer resubmitted unchanged after council feedback — finishing without re-deliberation; treat as UNVERIFIED",
					})
					a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
					unverifiedReason = "the same answer was resubmitted unchanged after council feedback, without re-deliberation"
				} else {
					keepWorking, unv := a.runCouncilGate(ctx, s, agent, turnTask, lastText, &councilRounds, &lastCouncilFeedback, buildCouncilChanges(guard.changeSet()), maxSteps-step, fab, time.Since(runStart), &councilSpent, &councilDeadlock)
					if keepWorking {
						prevFinishText, prevFinishCalls = lastText, guard.callCount()
						continue
					}
					// The gate is letting the turn finish. A non-empty reason means it did so
					// WITHOUT approving (deadlock/cost-cap/no-feedback/unavailable) — carry that
					// into turn.finished below so the UI shows UNVERIFIED, not a confident done.
					unverifiedReason = unv
					if !recovered && councilDeadlock &&
						strings.TrimSpace(lastCouncilFeedback) != "" && a.planEligible(agent, depth) {
						// Council deadlock: the members used every round and never approved, still holding
						// an unmet concern — the gate flags this explicitly (councilDeadlock), so a DONE
						// vote that merely landed on the last allowed round does NOT reach here. The agent
						// is stuck against a wall it could not clear on its own; before finishing UNVERIFIED,
						// hand the task plus that exact concern to a fresh child that re-plans and breaks it
						// down (ADaPT failure-recursion, solo-stuck branch). Fires once; on success, reset
						// the stall window and loop so the parent integrates and verifies the child's work.
						task := strings.TrimSpace(turnTask)
						if task == "" {
							task = strings.TrimSpace(lastUserText(reconstruct(a.taskEvents(sid, evs))))
						}
						if a.redecomposeStuck(ctx, s, agent, task, lastCouncilFeedback, depth) {
							recovered = true
							guard.resetStall()
							continue
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
			// Turn-cumulative usage (§8.1): out/cost summed across steps, in = last.
			u := event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
			// A finish the council never approved (deadlock/cost-cap/resubmit) lands as
			// UNVERIFIED so the UI stops painting an abandoned task as a confident done.
			d, _ := json.Marshal(event.TurnFinishedData{Usage: u, Unverified: unverifiedReason != "", Reason: unverifiedReason})
			a.appendFact(ctx, sid, event.TypeTurnFinished, agentActor, d)
			finished = true // the turn is over (approved done, or an honest UNVERIFIED landing)
			return lastText, nil
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
			if kind == "stall" && !recovered && a.planEligible(agent, depth) {
				task := strings.TrimSpace(turnTask)
				if task == "" {
					task = strings.TrimSpace(lastUserText(reconstruct(a.taskEvents(sid, evs))))
				}
				if a.redecomposeStuck(ctx, s, agent, task,
					"repeated no-progress: the previous attempt could not advance the task", depth) {
					recovered = true
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

// applyRoute computes the turnTask after routing a mid-turn interjection. "redirect"
// re-anchors on the interjection; "append" folds it into the current task; anything else
// ("queue"/"") leaves the task unchanged. changed reports whether the anchor moved (and
// thus whether the caller should absorb the interjection and reground).
func applyRoute(action, turnTask, interject string) (newTask string, changed bool) {
	switch action {
	case "redirect":
		return strings.TrimSpace(interject), true
	case "append":
		return strings.TrimSpace(turnTask + "\n\n" + interject), true
	default:
		return turnTask, false
	}
}

// injectInterjectDirective tells the agent a new user message arrived mid-turn. When
// deferred (not dispatching) it has been QUEUED to run after the current task, so the
// agent keeps focus instead of oscillating between the two (the live-observed thrash:
// plexus #7–#10) and may call route_interjection to redirect/append when confident.
// When dispatching (background subagents running, agent otherwise idle) the message is
// left visible and the agent is invited to answer it briefly without abandoning the task.
func (a *App) injectInterjectDirective(ctx context.Context, sid session.SessionID, turnTask, interject string, dispatching bool) {
	var text string
	if dispatching {
		// The orchestrator is otherwise idle while background subagents work, so it is free
		// to be responsive. Let it answer a short interjection inline WITHOUT abandoning or
		// folding it into the delegated task (which would corrupt that deliverable).
		text = "magi runtime note (not user input): a new user message arrived while the background subagents " +
			"you dispatched are still running:\n" + clipSpec(interject, 800) + "\n\n" +
			"You are otherwise idle until they report, so you MAY answer this briefly right now (e.g. a question or " +
			"a greeting). Do NOT abandon the delegated task, and do NOT fold this into its deliverable:\n" +
			clipSpec(turnTask, 800) + "\n\n" +
			"Answer only this aside — do NOT start reading/grepping or investigating the task yourself while the " +
			"subagents run; they own that work and duplicating it wastes turns. " +
			"If it is actually a new substantive task, say you will take it up after the current one finishes, then " +
			"keep coordinating the subagents."
	} else {
		text = "magi runtime note (not user input): a new user request arrived while you are mid-task:\n" +
			clipSpec(interject, 800) + "\n\n" +
			"It has been QUEUED and will run as its own turn after you finish the current task:\n" +
			clipSpec(turnTask, 800) + "\n\n" +
			"Keep working on the current task; do not switch away from it. If — and only if — you are confident the new " +
			"request should change your direction NOW, or be folded into the current task, call route_interjection " +
			"(action \"redirect\" or \"append\") with a one-line reason. When unsure, do nothing and it stays queued."
	}
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
}

// asideHandlerSystem drives the idle-park interjection handler: a focused, tool-capable turn
// that either replies briefly (chitchat) or SIGNALS a change of course, without doing the
// delegated work itself. It runs in fresh, minimal context (just the aside + a clip of the
// task for reference) so a reply is guaranteed to flush — the original bug was the main
// synthesis turn, handed the full task, deprioritizing conversational replies and dropping
// them for the entire delegated task. Only signal/interaction tools (route_interjection,
// cancel_dispatch, ask_user) are offered — never read/bash/write/task — so the model cannot
// start (or duplicate) the subagents' work here; the real re-plan/re-dispatch resumes in the
// next normal step, which regains the full toolset.
const asideHandlerSystem = "You dispatched background subagents and are now idle, waiting for them to report. " +
	"While you wait, the user sent you the message below. Handle ONLY this message — do NOT read files, run " +
	"commands, or investigate the task here; the subagents own that work and duplicating it wastes turns.\n\n" +
	"- If it is PURELY small talk or a standalone question unrelated to the task (a greeting, trivia), reply " +
	"briefly (one or two sentences) and end your turn with no tool call.\n" +
	"- If it touches the work in ANY way — narrows or widens scope, changes which files/directories/targets are in " +
	"play, adds or drops a constraint, reorders, or switches the goal — you MUST call route_interjection. A text " +
	"acknowledgment like \"got it, I'll focus on X\" does NOT change what the running subagents or the plan do; " +
	"acknowledging without routing silently DROPS the steer and the off-scope work continues. So: call " +
	"route_interjection to set the direction — \"redirect\" to switch to it now, \"append\" to fold it into the " +
	"current task so both are satisfied (ordering words like \"before\"/\"after\" are honored when you re-plan), or " +
	"\"queue\" to defer it to its own later turn — and keep results already produced. If running subagents are now " +
	"doing work the steer made irrelevant (e.g. reading files outside a newly narrowed scope), also call " +
	"cancel_dispatch to stop them so the re-plan re-dispatches under the new scope.\n" +
	"  Example: while explorers read the whole repo, the user says \"only the docs directory\" — that narrows scope, " +
	"so call route_interjection \"append\" (and cancel_dispatch the explorers reading outside docs), do NOT merely " +
	"reply \"got it\".\n" +
	"- If the request is ambiguous, call ask_user to clarify before routing.\n\n" +
	"The actual re-planning and re-dispatch happen in your normal turn after this — here you only reply or signal."

// maxAsideSteps caps the idle-park handler's mini-loop so it always terminates: enough to
// ask_user and then route in the same handling, but bounded against a tool-call loop.
const maxAsideSteps = 4

// asideEffect captures what an idle-park aside handling actually did to the running work, so
// handleAside can both set its queue disposition AND persist a durable audit record — the raw
// tool call/result parts stay in the mini-loop (to keep the delegated task's log clean), so
// without this the effect (a route redirect/append, a cancel) would leave no trace at all.
type asideEffect struct {
	route     string // route_interjection action that fired (redirect/append/queue), "" if none
	reason    string // route/cancel reason as given by the model
	cancelled int    // subagents stopped via cancel_dispatch
	didRoute  bool   // a redirect/append fired (breaks the park, re-plans next step)
	didCancel bool   // a cancel_dispatch fired
	escalate  bool   // modeQueued: the model routed → run the steer as its own top-level turn
}

// triageMode selects how the shared interjection mini-turn (interjectTurn) wires
// route_interjection and what disposition its caller applies.
type triageMode int

const (
	// modeAside: the orchestrator is idle-parked on its own subagents mid-turn. route_interjection
	// signals turnControl so the parked turn re-anchors/re-plans; a reply is chitchat.
	modeAside triageMode = iota
	// modeQueued: the turn has ended and a queued steer is being drained. route_interjection means
	// "this needs real work" → escalate to its own fresh top-level turn; a reply answers it inline.
	modeQueued
)

// handleAside runs a focused, tool-capable turn for a user interjection that arrived while the
// orchestrator is idle-parked on its own explorers. It replies to chitchat OR signals a steer
// (route_interjection / cancel_dispatch), optionally clarifying first via ask_user. The aside
// MUST already be enqueued (enqueue-first) so route_interjection — which requires a pending
// interjection — can fire. It returns whether it ACTED (route redirect/append, or a cancel):
// true means the caller should break the park so the next normal step drains the route and
// re-dispatches with the full toolset; false means re-park (a chitchat reply, a bare "queue",
// or nothing usable). Queue disposition is handled here: a redirect/append is left queued for
// the loop's turnControl drain to consume; a resolved chitchat reply or a bare cancel is
// consumed so it does not also re-run as its own turn; a defer/failure is left queued (no loss).
func (a *App) handleAside(ctx context.Context, agent AgentSpec, s session.Session, depth int, turnTask, aside string) (acted bool) {
	sys := asideHandlerSystem
	if t := strings.TrimSpace(turnTask); t != "" {
		sys += "\n\nThe background task (context only — do not act on it here):\n" + clipSpec(t, 500)
	}
	replied, eff := a.interjectTurn(ctx, agent, s, depth, sys, aside, modeAside)
	switch {
	case eff.didRoute:
		return true // redirect/append: the loop's turnControl drain consumes + re-anchors next step
	case eff.didCancel:
		a.consumeInterject(s.ID, aside) // cancel with no re-anchor: resolved here
		return true
	case replied:
		a.consumeInterject(s.ID, aside) // chitchat: answered here, don't also re-run as a turn
		return false
	default:
		return false // bare "queue" or nothing usable: leave queued to run later (no loss)
	}
}

// queuedTriageSystem drives the finish-boundary triage of a dequeued steer (modeQueued). The
// previous task is done, so the model either answers a question/chitchat from the session's
// recent context, or routes (any action) to hand real work to a fresh, fully-tooled turn. Safe
// default is to route: a needless fresh turn is cheap, a dropped task is not.
const queuedTriageSystem = "A user message was queued while you were finishing the previous task, which is now " +
	"complete. Handle ONLY this message and decide:\n" +
	"- If it is a question, a greeting, or otherwise fully answerable from the conversation so far, ANSWER it now in " +
	"one or two sentences and end your turn with NO tool call.\n" +
	"- If it needs real work — editing files, running commands, investigating the codebase, or anything you cannot " +
	"answer from what you already know — call route_interjection (any action) with a one-line reason. Do NOT attempt " +
	"the work here; routing hands it to a fresh, fully-tooled turn.\n" +
	"When unsure, route it — a needless fresh turn is cheap, a dropped task is not."

// triageQueued runs the shared interjection mini-turn on a steer dequeued at the finish
// boundary and reports whether it must ESCALATE to its own top-level turn. A question or
// chitchat is answered inline here (in the session's own recent context, no fresh-slate
// reset — so a follow-up like "how many files did you change?" keeps the task context) and
// returns false. Anything the model routes, or that produces nothing usable, returns true so
// the drain resurfaces it as a fresh turn. The safe default is escalate: no work is dropped.
func (a *App) triageQueued(ctx context.Context, agent AgentSpec, s session.Session, aside string) (escalate bool) {
	sys := queuedTriageSystem
	if tail := a.recentTranscript(ctx, s.ID, 8, 2000); tail != "" {
		sys += "\n\nRecent conversation (for context — do not re-answer it):\n" + tail
	}
	replied, eff := a.interjectTurn(ctx, agent, s, 0, sys, aside, modeQueued)
	if eff.escalate {
		return true // routed → run it as its own fully-tooled turn
	}
	if replied {
		return false // answered inline from context — the drain consumes it (pops the queue)
	}
	return true // nothing usable → run it as its own turn rather than risk dropping real work
}

// recentTranscript renders the last maxMsgs reconstructed messages of a session as compact
// "role: text" lines, byte-bounded by maxBytes, for use as read-only context in an isolated
// mini-turn (e.g. finish-boundary triage). Returns "" if the session cannot be read.
func (a *App) recentTranscript(ctx context.Context, sid session.SessionID, maxMsgs, maxBytes int) string {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return ""
	}
	msgs := reconstruct(evs)
	if len(msgs) > maxMsgs {
		msgs = msgs[len(msgs)-maxMsgs:]
	}
	var b strings.Builder
	for _, m := range msgs {
		if txt := strings.TrimSpace(partsText(m.Parts)); txt != "" {
			fmt.Fprintf(&b, "%s: %s\n", m.Role, clipLine(txt, 400))
		}
	}
	return clipSpec(strings.TrimSpace(b.String()), maxBytes)
}

// interjectTurn runs the shared focused mini-turn for a user interjection: it offers only the
// signal/interaction tools (route_interjection/cancel_dispatch/ask_user), streams a reply,
// executes any tool calls against a minimal env (no execution tools, so it cannot do delegated
// work here), persists a durable effect trace, and returns whether it produced a text reply
// plus the accumulated effect. Queue disposition (consume vs escalate vs break-park) is the
// caller's, since it differs by mode. mode selects how route_interjection is wired: modeAside
// signals turnControl to re-anchor the parked turn; modeQueued marks escalate.
func (a *App) interjectTurn(ctx context.Context, agent AgentSpec, s session.Session, depth int, sys, aside string, mode triageMode) (replied bool, eff asideEffect) {
	// Signal/interaction tools only — the model can reply or change course but cannot start
	// (or duplicate) delegated work here.
	var specs []port.ToolSpec
	for _, name := range []string{"route_interjection", "cancel_dispatch", "ask_user"} {
		if t, ok := a.tools.Get(name); ok {
			specs = append(specs, port.ToolSpec{Name: name, Description: t.Description(), Schema: t.Schema()})
		}
	}
	actor := event.Actor{Kind: event.ActorAgent, ID: orDefault(agent.Name, "default")}
	msgs := []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: aside}}}}
	for step := 0; step < maxAsideSteps; step++ {
		req := port.ChatRequest{Model: s.Model.Model, System: sys, Messages: msgs, Tools: specs}
		stream, err := a.providerFor(agent).StreamChat(ctx, req)
		if err != nil {
			break
		}
		msgID := "m_" + newID()
		textPartID := "p_" + newID()
		// Drain the stream ourselves rather than via consumeStream: this isolated turn must not
		// overwrite the session's real context-size meter with its tiny request, nor append a
		// stray TypeError to the delegated task's log on a transient failure — on error we stop.
		var text strings.Builder
		var calls []session.ToolCall
		failed := false
		for ev := range stream {
			switch ev.Type {
			case port.ProviderText:
				text.WriteString(ev.Text)
				d, _ := json.Marshal(event.PartDeltaData{MessageID: msgID, PartID: textPartID, Kind: session.PartText, Text: ev.Text})
				a.publishTransient(s.ID, event.TypePartDelta, actor, d)
			case port.ProviderToolCall:
				if ev.ToolCall != nil {
					calls = append(calls, *ev.ToolCall)
				}
			case port.ProviderError:
				failed = true
			}
		}
		if failed {
			break
		}
		reply := strings.TrimSpace(text.String())
		// Persist visible text (a chitchat reply, or a brief ack before a route) so it streams
		// and stays in the transcript. Tool-call/result parts are kept only in this mini-loop's
		// local context — not persisted — to avoid polluting the delegated task's log; the tool
		// EFFECTS (turnControl route, cancel notices) reach the loop through their own channels.
		if reply != "" {
			a.appendPart(ctx, s.ID, actor, msgID, session.RoleAssistant, session.Part{ID: textPartID, Kind: session.PartText, Text: reply})
			replied = true
		}
		if len(calls) == 0 {
			break // final turn: replied (or produced nothing) — done
		}
		asgParts := []session.Part{}
		if reply != "" {
			asgParts = append(asgParts, session.Part{ID: textPartID, Kind: session.PartText, Text: reply})
		}
		for i := range calls {
			c := calls[i]
			asgParts = append(asgParts, session.Part{Kind: session.PartToolCall, ToolCall: &c})
		}
		msgs = append(msgs, session.Message{Role: session.RoleAssistant, Parts: asgParts})
		for i := range calls {
			c := calls[i]
			res := a.execAsideTool(ctx, s, depth, &c, &eff, mode)
			msgs = append(msgs, session.Message{Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: &res}}})
		}
	}
	// Persist a durable, auditable trace of the steer's EFFECT (system actor, so interjection
	// detection — ActorUser only — ignores it). Uses WithoutCancel so the record survives even
	// if this handling raced a cancellation. Pure modeQueued escalation leaves no trace here —
	// the drain's resurfaced prompt is itself the record.
	if eff.didRoute || eff.didCancel {
		var b strings.Builder
		b.WriteString("Steer applied (not user input): ")
		if eff.didRoute {
			fmt.Fprintf(&b, "route_interjection %q", eff.route)
		}
		if eff.didCancel {
			if eff.didRoute {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "cancel_dispatch stopped %d subagent(s)", eff.cancelled)
		}
		if r := strings.TrimSpace(eff.reason); r != "" {
			fmt.Fprintf(&b, " — %s", clipSpec(r, 300))
		}
		fmt.Fprintf(&b, "\nInterjection: %s", clipSpec(strings.TrimSpace(aside), 300))
		_ = a.appendPromptText(context.WithoutCancel(ctx), s.ID, event.Actor{Kind: event.ActorSystem, ID: "steer"}, b.String())
	}
	return replied, eff
}

// execAsideTool executes one signal/interaction tool call from the idle-park handler against a
// minimal ToolEnv (only route/cancel/ask_user wired; every execution tool is nil, so the model
// cannot do delegated work here). It records which class of action fired so handleAside can set
// its return and queue disposition. Mirrors the routeInterjectionFn/cancelDispatchFn closures
// in execute.go so behavior (pending-interjection requirement, turnControl signal) is identical.
func (a *App) execAsideTool(ctx context.Context, s session.Session, depth int, c *session.ToolCall, eff *asideEffect, mode triageMode) session.ToolResult {
	env := port.ToolEnv{
		SessionID: s.ID,
		RouteInterjection: func(action, reason string) error {
			if mode == modeQueued {
				// The turn has already ended; there is no running turn to re-anchor. Any route
				// action here simply means "this needs real work" — mark it so the drain runs the
				// steer as its own fresh, fully-tooled turn.
				eff.escalate = true
				eff.route = action
				if reason != "" {
					eff.reason = reason
				}
				return nil
			}
			if !a.hasPendingInterject(s.ID) {
				return fmt.Errorf("there is no queued interjection to route right now")
			}
			a.signalTurnControl(s.ID, func(tc *turnControl) {
				tc.route = action
				if reason != "" {
					tc.reason = reason
				}
			})
			eff.route = action
			if reason != "" {
				eff.reason = reason
			}
			if action == "redirect" || action == "append" {
				eff.didRoute = true // "queue" changes nothing, so it neither routes nor breaks the park
			}
			return nil
		},
		CancelDispatch: func(agent, reason string) (int, error) {
			n, err := a.cancelDispatched(ctx, s.ID, agent, reason)
			if err == nil {
				eff.didCancel = true
				eff.cancelled += n
				if reason != "" && eff.reason == "" {
					eff.reason = reason
				}
			}
			return n, err
		},
		AskUser: a.askUserFn(ctx, s, depth, c),
	}
	tool, ok := a.tools.Get(c.Name)
	if !ok {
		b, _ := json.Marshal("unknown tool: " + c.Name)
		return session.ToolResult{CallID: c.CallID, Content: b, IsError: true}
	}
	res, err := tool.Execute(ctx, c.Args, env)
	if err != nil {
		b, _ := json.Marshal(err.Error())
		return session.ToolResult{CallID: c.CallID, Content: b, IsError: true}
	}
	res.CallID = c.CallID
	return res
}

// maxReplansPerTurn caps agent-initiated replans so replan cannot indefinitely reset
// the stall guard (the abuse vector: replan→reset→thrash→replan). Past the cap the
// stall guard is left intact and genuine thrash force-stops normally.
const maxReplansPerTurn = 2

// honorReplan applies an agent-initiated replan under an anti-abuse budget: at most
// maxReplansPerTurn per turn, and only when real tool work happened since the previous
// replan (back-to-back replans without action are churn). When honored it rebuilds the
// plan and resets the stall/council accounting (reground); when refused it injects
// guidance and leaves the stall guard intact.
func (a *App) honorReplan(ctx context.Context, sid session.SessionID, reason string, count, atCalls *int, curCalls int, reground func(bool)) {
	inject := func(msg string) {
		pd, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: "m_" + newID(),
			Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
		})
		_ = a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
	}
	if *count >= maxReplansPerTurn {
		inject(fmt.Sprintf("Replan refused: you have already replanned %d times this turn. Do not replan again — make "+
			"concrete progress on the current plan, or if you are truly blocked, report status \"failed\" and state exactly "+
			"what stopped you.", *count))
		return
	}
	// Require real tool work between replans. guard.callCount() counts EVERY tool call,
	// including the replan call that raised this signal, so a back-to-back replan-only step
	// still advances curCalls by exactly 1 (its own call) over the last honored replan's
	// snapshot. Anything at-or-below that +1 means nothing but the replan itself happened —
	// churn — so refuse; genuine work (bash/edit/read) lands curCalls at atCalls+2 or more.
	if *atCalls >= 0 && curCalls <= *atCalls+1 {
		inject("Replan refused: you replanned again without taking any real action since the last replan. Actually " +
			"attempt the current plan (run a command, edit a file, inspect why it failed) before deciding it is unworkable.")
		return
	}
	*count++
	*atCalls = curCalls
	note := "Replanning at your request"
	if r := strings.TrimSpace(reason); r != "" {
		note += ": " + clipLine(r, 200)
	}
	note += ". The plan and the no-progress window have been reset — decompose a fresh approach and proceed."
	inject(note)
	reground(true)
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

// lastUserPromptText returns the text of the most recent GENUINE user prompt
// (Actor.Kind == user), skipping council/hook/auto injections (which are recorded
// as user-role prompts but authored by the system). Used for the language lock so
// injected English feedback can't unlock the user's language.
func lastUserPromptText(evs []event.Event) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == event.TypePromptSubmitted && evs[i].Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(evs[i].Data, &d) == nil {
				return partsText(d.Parts)
			}
		}
	}
	return ""
}

// countUserPrompts counts genuine user (ActorUser) prompts in the event log. The loop
// snapshots this at step 0 and watches for a rise, which signals a mid-turn interjection.
func countUserPrompts(evs []event.Event) int {
	return len(userPromptTexts(evs))
}

// userPromptTexts returns the text of every genuine user (ActorUser) prompt in log
// order. The loop diffs its length against handledUserPrompts to enqueue EACH new
// mid-turn interjection, even when several land during a single blocked step (a
// count-only check would advance past the earlier ones and drop them).
func userPromptTexts(evs []event.Event) []string {
	entries := userPromptEntries(evs)
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Text
	}
	return out
}

// userPrompt is a genuine user prompt with the id of the event that carried it, so the
// interjection detector can mask that exact event while the message stays queued.
type userPrompt struct {
	MsgID string
	Text  string
}

// seedPromptIdx returns the index (in userPromptEntries order) of the genuine user
// prompt that SEEDS the current top-level turn: the first user prompt not already
// answered by an assistant reply. It is meaningful only at step 0, where the current
// turn has produced no output yet — so any assistant (ActorAgent) part in the log
// belongs to a PREVIOUS turn, and the first user prompt after the last such part is
// this turn's seed. Later user prompts are mid-turn interjections that piled up before
// execution began. Returns -1 when the log has no genuine user prompt (e.g. a subagent
// session, whose seed is authored by ActorAgent).
func seedPromptIdx(evs []event.Event) int {
	ui := -1           // running index into userPromptEntries order
	lastAnswered := -1 // highest user-prompt index a prior assistant reply covered
	for _, e := range evs {
		switch {
		case e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser:
			ui++
		case e.Type == event.TypePartAppended && e.Actor.Kind == event.ActorAgent:
			if ui >= 0 {
				lastAnswered = ui
			}
		}
	}
	if ui < 0 {
		return -1
	}
	seed := lastAnswered + 1
	if seed > ui {
		seed = ui // defensive: everything already answered → treat the latest as seed
	}
	return seed
}

// enqueueLateInterjections re-reads the store at the finish boundary and queues any
// genuine user prompt that appeared past the baseline (handled) — a steer that landed
// after this step's top-of-loop scan but before the turn committed (final-step stream,
// or a council round that then voted done). Such a prompt was never enqueued and is
// invisible to the run goroutine's last-message-role safety net, so without this it is
// silently lost. Enqueuing (and masking) it makes the pending-interjection drain run it
// as its own fresh top-level turn — the same disposition an ordinary deferred steer gets
// — instead of dropping it. Mirrors the top-of-loop deferral (skip empty / == turnTask).
func (a *App) enqueueLateInterjections(ctx context.Context, sid session.SessionID, handled int, turnTask string) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return
	}
	prompts := userPromptEntries(evs)
	if len(prompts) <= handled {
		return
	}
	// If the last message is itself a user prompt, the run goroutine's hasUnansweredUserPrompt
	// safety net already re-runs the loop, whose top-of-loop scan handles every late prompt —
	// enqueuing here too would run it a second time (a spurious duplicate turn). Act ONLY when a
	// late prompt is buried under a trailing non-user message: the exact blind spot of that net.
	if msgs := reconstruct(evs); len(msgs) == 0 || msgs[len(msgs)-1].Role == session.RoleUser {
		return
	}
	task := strings.TrimSpace(turnTask)
	for _, it := range prompts[handled:] {
		if txt := strings.TrimSpace(it.Text); txt != "" && txt != task {
			a.markInterjectSeen(sid, it.MsgID)
			a.enqueueInterject(sid, it.MsgID, txt)
		}
	}
}

// userPromptEntries returns every genuine user (ActorUser) prompt in log order, each
// paired with its PromptSubmitted MessageID.
func userPromptEntries(evs []event.Event) []userPrompt {
	var out []userPrompt
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				out = append(out, userPrompt{MsgID: d.MessageID, Text: partsText(d.Parts)})
			}
		}
	}
	return out
}

// currentTaskText is the query for push-side shard hints: the most recent genuine
// user prompt (what the user asked) joined with the latest assistant message (what
// the agent is doing right now). Using both means a hint can fire on the file the
// agent just started editing, not only on words from the opening prompt — the case
// where a weak model has drifted onto a sub-task and most needs the recalled detail.
func currentTaskText(evs []event.Event) string {
	prompt := lastUserPromptText(evs)
	var last string
	msgs := reconstruct(evs)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == session.RoleAssistant {
			var b strings.Builder
			for _, p := range msgs[i].Parts {
				if p.Kind == session.PartText {
					b.WriteString(p.Text + " ")
				}
			}
			if s := strings.TrimSpace(b.String()); s != "" {
				last = s
				break
			}
		}
	}
	return strings.TrimSpace(prompt + " " + last)
}

// lastUserText returns the text of the most recent user message.
func lastUserText(msgs []session.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == session.RoleUser {
			var b strings.Builder
			for _, p := range msgs[i].Parts {
				if p.Kind == session.PartText {
					b.WriteString(p.Text)
				}
			}
			return b.String()
		}
	}
	return ""
}

// experiencePointer renders the one-line push notice: how many stored memories/skills
// match, pointing the agent at recall_memory to pull the detail. Empty when nothing
// matched, so the section is dropped entirely.
func experiencePointer(nMem, nSkill int) string {
	total := nMem + nSkill
	if total == 0 {
		return ""
	}
	noun := "entry"
	if total != 1 {
		noun = "entries"
	}
	return fmt.Sprintf("%d relevant team memory/skill %s exist — call recall_memory with keywords to read the details.", total, noun)
}

// formatExperienceFull renders the full memory/skill entries for a recall_memory pull
// (as opposed to the one-line push pointer). This is where the detail actually enters
// context, and only when the agent asked for it.
func formatExperienceFull(mems []port.Memory, skills []port.Skill) string {
	var b strings.Builder
	for _, m := range mems {
		b.WriteString("- " + strings.TrimSpace(m.Text) + "\n")
	}
	for _, s := range skills {
		b.WriteString("- skill " + s.Name + ": " + strings.TrimSpace(s.Description) + "\n")
		if body := strings.TrimSpace(s.Body); body != "" {
			b.WriteString("  " + strings.ReplaceAll(body, "\n", "\n  ") + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatTodos renders the plan as a checklist for the system prompt.
func formatTodos(td []session.Todo) string {
	mark := map[string]string{"completed": "[x]", "in_progress": "[~]", "pending": "[ ]", "cancelled": "[✗]"}
	var b strings.Builder
	for i, t := range td {
		if i > 0 {
			b.WriteString("\n")
		}
		m := mark[t.Status]
		if m == "" {
			m = "[ ]"
		}
		b.WriteString(m + " " + t.Content)
	}
	return b.String()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
