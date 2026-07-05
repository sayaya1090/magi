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
	stopChecked := false              // Stop hooks enforced at most once per run
	nudgedEmpty := false              // subagent empty-result nudge fired at most once
	selfVerified := false             // pre-finish self-verify self-prompt (fallback path) fired at most once per run
	reviewGateFired := false          // has the delegated review gate fired yet this run?
	reviewBudget := reviewSpawnBudget // remaining delegated-review subagents this run (spent across re-arm firings)
	reviewedAtEpoch := 0              // guard.mutationEpoch() at the last review-gate firing (re-arm only on a NEW mutation since — incl. bash writes)
	reviewPassedEpoch := -1           // mutationEpoch at which the tester last returned VERDICT: PASS; -1 = never. Fresh-evidence: finish needs reviewPassedEpoch == current epoch
	reportRefused := false            // a subagent's unverified "done" report was refused once this run
	evidencePushed := false           // execution-evidence gate's one-shot "run it for real" push fired at most once per run
	recovered := false                // stuck-recovery redecompose (stall/council deadlock → depth+1 child) fired at most once per run
	guard := newRunGuard()
	councilRounds := 0               // consensus termination gate rounds this turn (D14)
	lastCouncilFeedback := ""        // last round's feedback (no-progress detection)
	prevFinishText := ""             // the answer the council rejected last round
	prevFinishCalls := -1            // guard.callCount() at that rejection (-1 = none yet)
	prevFinishFp := ""               // guard.progressFingerprint() at that rejection ("" = none yet); D1 direction-terminal
	councilSpent := time.Duration(0) // self-measured wall-clock consumed by deliberations
	councilDeadlock := false         // set by the gate iff its finish was a genuine round-cap deadlock (never approved)
	turnTask := ""                   // the user instruction THIS turn answers, snapshotted at
	// step 0 — so a steer that lands during the council gate can't hijack what the
	// council judges against (that interjection gets its own follow-up turn instead).
	usedTools := seedWork // did this turn do real work? (planner investigation seeds it; council skips pure conversational turns)
	usedMutator := false  // did this turn run a state-changing tool (bash or edit/write)? gates the pre-finish review/self-verify — bash writes files too, but don't reach guard.changeSet(). The review gate's re-arm keys off guard.mutationEpoch() (which counts edit/write AND bash writes), so a bash-authored fix re-triggers verification too.
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
		planned, delegated := a.maybePlanPreflight(ctx, s, depth, maxSteps)
		if planned {
			usedTools = true // planner did real work — seed the termination council
		}
		// A delegate wrote to the shared tree via a CHILD runLoop, so those changes are in
		// the child's guard, not this one's — this turn's changeSet/epoch stay empty. Seed
		// usedMutator so THIS (depth-0) turn's review-gate tester + council still fire and
		// verify the MERGED result (the tester inspects the working tree when the changeSet
		// is empty). Without this, a parent that only delegated would finish with no
		// independent execution check on the delegated work.
		if delegated {
			usedMutator = true
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
			turnTask = lastUserPromptText(evs) // the prompt that drove this turn
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
			Tools:    a.toolSpecs(agent, isSub),
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
			if tc.Name == "bash" || fileModifiers[tc.Name] {
				usedMutator = true // a write-capable tool ran; a bash write never reaches guard.changeSet()
			}
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
			// Pre-finish self-verification: a turn that produced a concrete artifact
			// (files changed) must confirm its ACTUAL output matches the task before the
			// council — which reviews text but cannot execute — ever sees it. Two passes:
			// coverage first, then correctness. Coverage targets the "did the first/easy
			// case, declared victory" failure — an agent that maps 1 of 10 mazes or edits
			// 1 of N files and then re-checks only what it made; correctness targets the
			// "claimed done, output actually wrong" failure (a placeholder, a filename
			// where the content was asked for, or a program that doesn't run). PASS 2 also
			// carries an anti-fabrication clause: a weak model, told to "verify", will
			// sometimes hand-write what a run WOULD have produced (e.g. inventing a maze it
			// never actually explored) to make the check appear to pass. The prompt forbids
			// that — verify only against output actually observed, and if it could not
			// run/observe something, mark it UNFINISHED rather than manufacture a
			// plausible-looking result. The prompt
			// makes the agent enumerate the task's own requirements/scope rather than
			// encoding any specific count — general to any multi-deliverable task. Fires
			// once per turn, top level only, and only when the turn ran a write-capable
			// tool (bash or edit/write) — a pure read/answer turn has no separate artifact
			// to re-check, so gating it would only churn. We key off usedMutator rather
			// than guard.changeSet() because bash writes files too but never populate the
			// changeSet (only edit/write/multiedit do), so a bash-driven agent that did
			// 1 of N deliverables would otherwise never get the coverage check.
			if depth == 0 && usedTools && usedMutator {
				// Delegated review gate: instead of asking the agent to self-verify (which
				// its own confirmation bias can wave through), dispatch independent read-only
				// tester+reviewer subagents to check the work with fresh eyes and inject their
				// findings. Falls back to the self-verify prompt when disabled or the reviewer
				// agents aren't configured.
				if a.cfg.ReviewGate && a.hasReviewGateAgents() {
					// Fresh-evidence completion gate: a turn that produced a deliverable may only
					// finish once the tester has independently RUN the verification and returned
					// PASS for the CURRENT deliverable version. "Version" is guard.mutationEpoch()
					// — it counts edit/write AND bash file-writes, so any later change (in any
					// tool) bumps it and makes a prior PASS stale, forcing re-verification. This is
					// what makes "verified" mean "verified THIS code", in ANY language: the gate
					// keys off the tester's real run, not off scanning the agent's prose for
					// English confession phrases.
					//
					// First fire on any state change; re-arm only on a NEW mutation since the last
					// firing (so re-inspecting findings with `git status`/`cat` can't burn budget).
					// A repeatedly-rejecting council can't spawn reviewers without end because the
					// per-run budget bounds the fan-out; once it is spent the council alone gates.
					epoch := guard.mutationEpoch()
					firstFire := !reviewGateFired
					reFire := reviewGateFired && epoch > reviewedAtEpoch
					if reviewPassedEpoch != epoch && reviewBudget > 0 && (firstFire || reFire) {
						reviewGateFired = true
						reviewedAtEpoch = epoch
						spawned, verdict := a.runReviewGate(ctx, s, turnTask, guard.changeSet(), reviewBudget)
						reviewBudget -= spawned
						if verdict == verdictPass {
							reviewPassedEpoch = epoch // behavioral evidence: THIS version was independently run and passed
						}
						continue // let the agent address the findings (or, on PASS, confirm and finish next round)
					}
					// Already freshly verified this epoch, or the budget is spent: fall through to
					// the council. A FAIL/BLOCKED the agent ignored without editing (no new epoch)
					// also lands here — the council, holding the injected findings + fabrication
					// evidence, is the bounded backstop rather than an unbounded re-verify loop.
				} else if !selfVerified {
					selfVerified = true
					msg := "Before finishing, VERIFY you satisfied the task literally — in two passes. " +
						"PASS 1 (coverage): re-read the original task and list every distinct thing it requires — each " +
						"deliverable, and any quantifier or scope it states (e.g. 'all', 'each', 'every', a range or list " +
						"of IDs/inputs, multiple files). Then confirm you actually did EACH one, not just the first or the " +
						"easy case. If the task asks for N things and you did fewer, it is INCOMPLETE — do the rest before " +
						"finishing. PASS 2 (correctness): for what you produced, confirm the ACTUAL result matches what the " +
						"task SPECIFIES — read the created/edited file(s) back and check their real content, or run the " +
						"program/command and check its behavior against the intended outcome. Verify ONLY against output you " +
						"actually observed this turn — file contents you really read back, or the real output of a command " +
						"you really ran. Do NOT invent, guess, or hand-write what a run WOULD have produced: if you could not " +
						"actually execute or observe something — an interactive program you never drove, a test you never " +
						"ran, a result you only assumed — then you have NOT verified it. Never fabricate its output or write " +
						"a stand-in/placeholder file to make this check appear to pass; that is worse than leaving it " +
						"undone, because it hides the gap. If the user explicitly asked for synthetic, fictional, or example " +
						"content, producing it is fine — but your final report must LABEL it as synthetic (\"this log is " +
						"fictional; the game was not actually run\"), never describe invented content as verified or real. " +
						"In that case say plainly that you could not run or confirm it, and " +
						"treat that requirement as UNFINISHED, not done. Match the task, not a generic " +
						"'it works': the intended state may be that something succeeds, but it may just as well be that it is " +
						"removed, disabled, rejects bad input, or fails on purpose. Fix any real mismatch — wrong content, " +
						"value, format, name, or location, or a leftover placeholder. If this check reveals your earlier " +
						"work was wrong, incomplete, or that you called it done too soon, say so plainly and fix it — do not " +
						"restate that it works or paper over the gap. Finish only once every requirement is done and its " +
						"result matches what the task asked for."
					// Structural, language-agnostic sharpening (replaces the old English phrase scan):
					// when the agent changed a deliverable but ran nothing exercising the current
					// version, lead with that concrete fact rather than a generic "verify" — this is
					// the fallback path (no tester agents), so this prompt is the only execution push.
					if guard.unverifiedDeliverable() {
						msg = "You changed a deliverable this turn but have run NO command that exercises the current " +
							"version, so nothing here is verified by execution yet. Actually run it now — execute the " +
							"program or its tests and read the REAL output — then confirm it against the task. If there is " +
							"genuinely nothing to run, say so plainly and do not describe it as verified or done.\n\n" + msg
					}
					pd, _ := json.Marshal(event.PromptSubmittedData{
						MessageID: "m_" + newID(),
						Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
					})
					a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
					continue
				}
			}
			// Direction-terminal short-circuit (D1): the agent came back to finish with its
			// deliverable CONTENT unchanged since its last finish attempt (progressFingerprint
			// frozen) and still no independent PASS for that version (mutationEpoch !=
			// reviewPassedEpoch). Re-deliberating the council or re-spawning the tester cannot
			// help — it is not changing the artifact — so this is a terminal "not verified"
			// state, not a spin to keep pushing. Content-addressed (not epoch/text): it catches
			// the varied-echo / rewrite-same-bytes spin that the idle short-circuit below (which
			// needs byte-identical narration AND zero tool calls) and mutationEpoch both miss.
			// Only for file-deliverable turns (usedMutator); a text-only turn has no fingerprint
			// and keeps the byte-equality path. Fires from the SECOND frozen finish (prevFinishFp
			// is only recorded once the council let a finish through), so the council still gets
			// one full round before the turn lands honestly UNVERIFIED (see the evidence gate).
			// Scoped like the evidence gate to ReviewGate + tester agents: only an INDEPENDENT
			// verifier that has NOT passed the current version makes "unverified" a fact. Without
			// a tester (reviewPassedEpoch stays -1 forever) a genuinely-complete single-file task
			// that the council merely re-questions would be mislabeled, so there we defer to the
			// council/self-verify path unchanged.
			frozenUnverified := directionGateEnabled() && usedMutator &&
				a.cfg.ReviewGate && a.hasReviewGateAgents() && prevFinishFp != "" &&
				guard.progressFingerprint() == prevFinishFp && guard.mutationEpoch() != reviewPassedEpoch
			if frozenUnverified {
				dd, _ := json.Marshal(event.CouncilDecidedData{
					Round: councilRounds + 1, Decision: string(council.Done),
					Note: "deliverable content unchanged and still unverified by execution since the last finish attempt — finishing UNVERIFIED without further deliberation",
				})
				a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
			}
			// Consensus council termination gate (D14): top level only, not in
			// workflow mode, and only for turns that did real work — a purely
			// conversational reply (no tool use, e.g. a greeting) has nothing to
			// verify, so gating it just churns and can derail a weak model. Skipped
			// when D1 already resolved this finish as terminal-unverified above.
			if !frozenUnverified && depth == 0 && a.cfg.Council != nil && !a.cfg.Workflow && usedTools {
				// Structural fabrication evidence for the council: if the agent changed a
				// deliverable this turn but ran no command exercising the current version, that is a
				// hard, language-agnostic fact a text-only vote can't wave through. Replaces the old
				// English-only confession-phrase scan; the review-gate tester remains the authority.
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
				} else if a.runCouncilGate(ctx, s, agent, turnTask, lastText, &councilRounds, &lastCouncilFeedback, buildCouncilChanges(guard.changeSet()), maxSteps-step, fab, time.Since(runStart), &councilSpent, &councilDeadlock) {
					prevFinishText, prevFinishCalls = lastText, guard.callCount()
					prevFinishFp = guard.progressFingerprint() // D1: snapshot deliverable content so a next unchanged finish lands
					continue
				} else if !recovered && councilDeadlock &&
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
						usedMutator = true
						guard.resetStall()
						continue
					}
				}
			}
			// Execution-evidence gate (A)+(D): the council judges whether the work is GOOD
			// (quality on text+diff); this gate enforces the orthogonal, machine-checked fact of
			// whether the CURRENT version was actually RUN to a passing result this turn. A turn
			// that changed a deliverable may NOT land as a confident outcome — a success OR an
			// "impossible" give-up — on execution it never performed; the only honest terminal
			// state then is UNVERIFIED. The signal is the review-gate tester's fresh-evidence
			// verdict: reviewPassedEpoch == current mutationEpoch means an independent run passed
			// AFTER the last change. Only meaningful when the tester actually ran (ReviewGate +
			// agents present); the self-verify fallback has no independent verdict to stand on.
			unverified, unverifiedReason := false, ""
			if frozenUnverified {
				// D1 already ruled this finish terminal-unverified: the deliverable content did
				// not change since the last finish, so re-running the evidence push (which only
				// asks the agent to run the SAME unchanged artifact again) would just spin. Land
				// honestly now — the current best deliverable stays on disk for the grader.
				unverified = true
				unverifiedReason = "the deliverable content did not change across finish attempts and was not independently run to a passing result — outcome is unverified by execution"
			} else if depth == 0 && usedTools && usedMutator && evidenceGateEnabled() &&
				a.cfg.ReviewGate && a.hasReviewGateAgents() && guard.mutationEpoch() != reviewPassedEpoch {
				// Structural UNRUNNABLE (D4): the change set is entirely non-executable
				// (config/doc/data), so there is genuinely nothing to independently run — the
				// tester could never pass it, and pushing "actually run it now" only churns at a
				// deliverable that cannot be run. Land honestly now with a reason that says
				// "nothing to run" rather than "you failed to run it". Still UNVERIFIED: the pass
				// is never forced, the file just stays on disk for the grader. Conservative — any
				// code/script/unknown file in the set makes this false, so a real program is never
				// mislabeled. Under the same verdict-tier flag as the vacuous-pass demotion (D2).
				if verdictTierEnabled() && !guard.deliverableRunnable() {
					unverified = true
					unverifiedReason = "the change set is non-executable (config/doc/data), so there is nothing to independently run — delivered but unverified by execution"
				} else if !evidencePushed {
					// One bounded chance to make it real before we accept the finish: run the
					// current version and fix what breaks, or say plainly you could not. Never
					// loop on this — if it comes back still unverified, land honestly below.
					evidencePushed = true
					msg := "You are about to finish, but the current version of the deliverable has NOT been " +
						"independently run to a passing result this turn — so neither \"done\" nor \"cannot be done\" " +
						"is established yet. Actually run it now: execute the program or its tests against the REAL " +
						"deliverable and read the actual output. If it fails, fix it and run it again. If you " +
						"genuinely cannot run or confirm it here, do NOT claim success or declare it impossible — " +
						"say plainly what you could not verify and why, and leave it as unverified. Never invent " +
						"output or write a stand-in/placeholder to make it look done."
					pd, _ := json.Marshal(event.PromptSubmittedData{
						MessageID: "m_" + newID(),
						Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
					})
					a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
					continue
				} else {
					// Push already spent and still no passing run for the current version: finish,
					// but labeled honestly rather than laundered as a confident success.
					unverified = true
					unverifiedReason = "the deliverable was not independently run to a passing result this turn — outcome is unverified by execution"
				}
			}
			a.setStage(sid, stageFinalize) // turn is ending (D15)
			// Turn-cumulative usage (§8.1): out/cost summed across steps, in = last.
			u := event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
			d, _ := json.Marshal(event.TurnFinishedData{Usage: u, Unverified: unverified, Reason: unverifiedReason})
			a.appendFact(ctx, sid, event.TypeTurnFinished, agentActor, d)
			finished = true // genuine completion (council done / nothing more to do)
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
				clipLine(task, 1500)
			if kind == "stalled" {
				msg = "You've run many steps without changing anything or making concrete progress — you may be " +
					"re-running checks or restating the same conclusion instead of advancing the task. Stop and take a " +
					"DIFFERENT concrete action toward the deliverable; if something is blocking you, state exactly what " +
					"it is and why before continuing. Guess only when necessary: if a value is unknown but discoverable, " +
					"run the tool or command that determines it (compute, parse, crack, query, or read the real state) " +
					"rather than trying values blindly. Re-read the original task:\n" +
					clipLine(task, 1500)
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
					usedMutator = true
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

// formatExperience renders retrieved shared memories/skills for the prompt.
func formatExperience(mems []port.Memory, skills []port.Skill) string {
	var b strings.Builder
	for _, m := range mems {
		b.WriteString("- " + oneLineHint(m.Text) + "\n")
	}
	for _, s := range skills {
		b.WriteString("- skill " + s.Name + ": " + oneLineHint(s.Description) + "\n")
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
