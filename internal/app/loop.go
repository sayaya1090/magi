package app

import (
	"context"
	"encoding/json"
	"fmt"
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
func (a *App) consumeStream(ctx context.Context, sid session.SessionID, agentActor event.Actor, stream <-chan port.ProviderEvent, msgID, textPartID, reasonPartID string) (streamStep, error) {
	var text, reasoning strings.Builder
	var res streamStep
	streamErr := false
	for ev := range stream {
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
		case port.ProviderError:
			a.emitError(ctx, sid, agentActor, ev.Err.Error())
			streamErr = true
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
		planned, _ := a.maybePlanPreflight(ctx, s, depth, maxSteps)
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
			a.maybePlanPreflight(ctx, s, depth, maxSteps)
		}
	}

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			return lastText, ctx.Err()
		}
		a.setStage(sid, stageExecute) // tag this iteration's events as execute (D15)

		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			a.emitError(ctx, sid, agentActor, err.Error())
			return lastText, err
		}
		if step == 0 {
			turnTask = lastUserPromptText(evs)         // the prompt that drove this turn
			handledUserPrompts = countUserPrompts(evs) // baseline; a later rise is a mid-turn interjection
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
			// last absorbed one). Top level only — subagents aren't steered by the user. Queue it
			// by default (safe: preserves both tasks) and tell the agent it's deferred so it stops
			// oscillating; the agent may call route_interjection to redirect/append instead.
			if depth == 0 && !a.cfg.Workflow {
				prompts := userPromptTexts(evs)
				if len(prompts) > handledUserPrompts {
					// Enqueue EVERY user prompt that appeared since the last check, not just
					// the newest: two messages steered in during one long step would otherwise
					// advance the counter past the earlier one, dropping it silently.
					var newest string
					for _, it := range prompts[handledUserPrompts:] {
						if it = strings.TrimSpace(it); it != "" && it != strings.TrimSpace(turnTask) {
							a.enqueueInterject(sid, it)
							newest = it
						}
					}
					handledUserPrompts = len(prompts)
					if newest != "" {
						a.injectQueuingDirective(ctx, sid, turnTask, newest)
					}
				}
			}
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

		msgs := reconstruct(evs)
		// If auto-orchestration fires, it injects a directive as a new event; re-read
		// and rebuild msgs so the directive reaches the model in THIS turn, not the next.
		if a.checkAutoOrchestration(ctx, sid, depth, s.Model.Model, sys, msgs) {
			if evs2, err := a.store.Read(ctx, sid, 0); err == nil {
				evs = evs2
				msgs = reconstruct(evs)
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
							task = strings.TrimSpace(lastUserText(reconstruct(evs)))
						}
						if a.redecomposeStuck(ctx, s, agent, task, lastCouncilFeedback, depth) {
							recovered = true
							guard.resetStall()
							continue
						}
					}
				}
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
				task = strings.TrimSpace(lastUserText(reconstruct(evs)))
			}
			msg := "You've repeated the same no-progress action several times and are getting blocked. " +
				"Stop and change approach: try a different tool or a smaller step, or inspect WHY the last " +
				"attempts failed (read the error, check paths/state) before retrying. Re-read the original task:\n" +
				clipSpec(task, 1500)
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
					task = strings.TrimSpace(lastUserText(reconstruct(evs)))
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
	if a.autoOrchestrateActive[sid] {
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
		a.autoOrchestrateActive[sid] = true
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

// injectQueuingDirective tells the agent a new user request arrived mid-turn and has
// been QUEUED to run after the current task, so it keeps focus instead of oscillating
// between the two (the live-observed thrash: plexus #7–#10). The agent may call
// route_interjection to redirect (switch now) or append (fold in) when confident.
func (a *App) injectQueuingDirective(ctx context.Context, sid session.SessionID, turnTask, interject string) {
	text := "magi runtime note (not user input): a new user request arrived while you are mid-task:\n" +
		clipSpec(interject, 800) + "\n\n" +
		"It has been QUEUED and will run as its own turn after you finish the current task:\n" +
		clipSpec(turnTask, 800) + "\n\n" +
		"Keep working on the current task; do not switch away from it. If — and only if — you are confident the new " +
		"request should change your direction NOW, or be folded into the current task, call route_interjection " +
		"(action \"redirect\" or \"append\") with a one-line reason. When unsure, do nothing and it stays queued."
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
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
	var out []string
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				out = append(out, partsText(d.Parts))
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
