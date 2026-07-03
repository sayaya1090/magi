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
	// Pre-flight planner: when the task splits into independent areas, fan out
	// read-only explorers in parallel and inject their findings before the main
	// agent runs. Degrades to solo (no-op) when disabled or not parallelizable.
	planned := a.maybePlanPreflight(ctx, s)
	if a.cfg.Workflow {
		return a.runWorkflow(ctx, s)
	}
	// If the planner did investigation and injected findings, the turn already did
	// real work — seed it so the termination council convenes even when the main
	// agent only synthesizes the findings (no tools of its own).
	agent := a.agentFor(s)
	// Show the main agent working the next step (◐) for the rest of the turn — a
	// deterministic in_progress signal, since a weak model rarely calls todowrite.
	a.markFirstPendingActive(ctx, s.ID, event.Actor{Kind: event.ActorAgent, ID: orDefault(agent.Name, "default")})
	_, err := a.runLoop(ctx, s, agent, 0, 0, planned)
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
	reviewGateFired := false          // has the delegated review gate fired yet this run? (first fire counts bash; re-arm counts only new file mutations)
	reviewBudget := reviewSpawnBudget // remaining delegated-review subagents this run (spent across re-arm firings)
	reviewedAtMutators := 0           // mutatorCalls at the last review-gate firing (re-arm only on NEW file mutations since)
	fabNudges := 0                    // pre-finish fabrication refusals this run (max 2: redirect, then ultimatum)
	guard := newRunGuard()
	councilRounds := 0               // consensus termination gate rounds this turn (D14)
	lastCouncilFeedback := ""        // last round's feedback (no-progress detection)
	prevFinishText := ""             // the answer the council rejected last round
	prevFinishCalls := -1            // guard.callCount() at that rejection (-1 = none yet)
	councilSpent := time.Duration(0) // self-measured wall-clock consumed by deliberations
	turnTask := ""                   // the user instruction THIS turn answers, snapshotted at
	// step 0 — so a steer that lands during the council gate can't hijack what the
	// council judges against (that interjection gets its own follow-up turn instead).
	usedTools := seedWork // did this turn do real work? (planner investigation seeds it; council skips pure conversational turns)
	usedMutator := false  // did this turn run a state-changing tool (bash or edit/write)? gates the pre-finish self-verify — bash writes files too, but don't reach guard.changeSet()
	mutatorCalls := 0     // count of genuine file-mutating tool calls (edit/write, NOT read-only bash) — the re-arm marker: the review gate re-fires only when this grows past reviewedAtMutators (a real post-council fix), not on a re-inspection via `git status`/`cat`
	// Turn usage accumulation (§8.1): output tokens and cost sum across steps; input
	// is the last step's (the current context size, not a sum).
	var cumOut, lastIn int
	var cumCost float64

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
			if fileModifiers[tc.Name] {
				mutatorCalls++ // re-arm marker: a genuine file mutation (edit/write) — read-only bash is excluded so re-inspecting findings can't re-fire the gate
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
			// Pre-finish fabrication gate (deterministic, before the softer self-verify
			// nudge and the text-only council). If a file the agent WROTE this turn admits,
			// in its own body, that it is a stand-in rather than the real result — "in a
			// real implementation this would…", "since we can't actually run…" — do not let
			// the turn finish on it. This is the failure the council waved through on
			// blind-maze 5x5: the agent, unable to drive the interactive game, hand-wrote a
			// "sample maze" whose own comments confess it is simulated, then declared done.
			// Unlike the self-verify prompt (which the model can answer with more narration),
			// this keys off the model's OWN confession in the artifact, so it can't be talked
			// past. Fires once per turn; the injected prompt tells the agent to do the real
			// work or honestly report it could not — never to present the fake as done.
			if depth == 0 && fabNudges < 2 {
				if p, snip := guard.scanFabrication(); p != "" {
					fabNudges++
					var msg string
					if fabNudges == 1 {
						msg = fmt.Sprintf("Before finishing: %s contains text admitting it is not a real solution but a "+
							"stand-in — matched: %q. A placeholder or simulated output is NOT a completed task. Either do "+
							"the real work now — run the actual program/command and build the deliverable from its real "+
							"output — or, if you genuinely cannot, say so plainly and report the task as UNFINISHED. Do not "+
							"present fabricated or 'what a real run would produce' output as done.", p, snip)
					} else {
						// Second offense: the agent reworked the placeholder instead of replacing it.
						// Reworking a fake burns the whole budget (measured: blind-maze 5x5, gpt2-codegolf,
						// write-compressor all timed out polishing stand-ins) — so the second refusal is an
						// ultimatum: drop the placeholder entirely or fail honestly NOW, no third rewrite.
						msg = fmt.Sprintf("Still fabricated: %s again admits it is a stand-in — matched: %q. Do NOT "+
							"rewrite or polish the placeholder a third time; that only burns your remaining steps. "+
							"Choose now: (a) DELETE the stand-in and produce the deliverable from a REAL run of the "+
							"actual program/command, or (b) if that is truly impossible here, state plainly that the "+
							"task is UNFINISHED and why. There is no third option.", p, snip)
					}
					pd, _ := json.Marshal(event.PromptSubmittedData{
						MessageID: "m_" + newID(),
						Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
					})
					a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
					continue
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
					// First fire: any state-changing tool qualifies (incl. a bash write, which
					// never reaches the changeSet — hence usedMutator, not mutatorCalls, here).
					// Re-arm: only a NEW file mutation since the last firing (mutatorCalls counts
					// edit/write, not read-only bash), so re-inspecting the findings with
					// `git status`/`cat` can't burn budget on a pointless re-check. This lets a
					// council rejection's FIX get independently re-verified, while the per-run
					// budget bounds the fan-out so a repeatedly-rejecting council can't spawn
					// reviewers without end. Once the budget is spent, the council alone gates.
					firstFire := !reviewGateFired && usedMutator
					reFire := reviewGateFired && mutatorCalls > reviewedAtMutators
					if reviewBudget > 0 && (firstFire || reFire) {
						reviewGateFired = true
						reviewedAtMutators = mutatorCalls
						reviewBudget -= a.runReviewGate(ctx, s, turnTask, guard.changeSet(), reviewBudget)
						continue
					}
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
					pd, _ := json.Marshal(event.PromptSubmittedData{
						MessageID: "m_" + newID(),
						Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
					})
					a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
					continue
				}
			}
			// Consensus council termination gate (D14): top level only, not in
			// workflow mode, and only for turns that did real work — a purely
			// conversational reply (no tool use, e.g. a greeting) has nothing to
			// verify, so gating it just churns and can derail a weak model.
			if depth == 0 && a.cfg.Council != nil && !a.cfg.Workflow && usedTools {
				// Re-scan for a self-admitted fabrication (the gate above may have nudged the
				// agent, which then fixed it — or didn't): feed a still-present confession to the
				// council as hard, deterministic evidence so a text-only vote can't wave it through.
				fab := ""
				if p, snip := guard.scanFabrication(); p != "" {
					fab = p + ": " + snip
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
				} else if a.runCouncilGate(ctx, s, agent, turnTask, lastText, &councilRounds, &lastCouncilFeedback, buildCouncilChanges(guard.changeSet()), maxSteps-step, fab, time.Since(runStart), &councilSpent) {
					prevFinishText, prevFinishCalls = lastText, guard.callCount()
					continue
				}
			}
			a.setStage(sid, stageFinalize) // turn is ending (D15)
			// Turn-cumulative usage (§8.1): out/cost summed across steps, in = last.
			u := event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
			d, _ := json.Marshal(event.TurnFinishedData{Usage: u})
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
			// self-verify, the fabrication gate, and the council are all top-level only, and
			// this return fires before the len(toolCalls)==0 finish branch ever runs. So the
			// delegated path had NO fabrication check at all. Apply it here too — a "done"
			// report backed by a deliverable that confesses it is a stand-in is not done.
			// Refuse the report ONCE; push the subagent to do the real work or report failed.
			if rep.status == "done" && fabNudges == 0 {
				if p, snip := guard.scanFabrication(); p != "" {
					fabNudges = 2 // the report path refuses once; no pre-finish escalation after
					msg := fmt.Sprintf("You reported done, but %s contains text admitting it is not a real "+
						"solution but a stand-in — matched: %q. A placeholder or simulated deliverable is NOT a "+
						"completed task. Either do the real work now — actually run the required program/command and "+
						"produce the genuine result — or, if you truly cannot, report status \"failed\" and say plainly "+
						"what stopped you. Do not report fabricated work as done.", p, snip)
					pd, _ := json.Marshal(event.PromptSubmittedData{
						MessageID: "m_" + newID(),
						Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
					})
					a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
					continue
				}
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

		// Loop guard: stop gracefully rather than burning the full step budget,
		// either on hard repeats or on a stall the agent kept ignoring after every
		// nudge was spent (varied-but-unproductive calls never trip the repeat count).
		switch guard.stuck() {
		case "repeat":
			d, _ := json.Marshal(event.ErrorData{Message: "stopped: the agent repeated the same action without progress (loop guard)", Code: "loop_guard"})
			a.appendFact(ctx, sid, event.TypeError, event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
			return lastText, nil
		case "stall":
			d, _ := json.Marshal(event.ErrorData{Message: "stopped: no real progress after repeated redirection (stall guard)", Code: "stall_guard"})
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
