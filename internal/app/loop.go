package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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
	//
	// The caller hands the MASKED event view (liveEvents), the same one the messages are built from.
	// Anchored on the raw list, a deferred mid-turn interjection — hidden from the messages and from
	// the turn task — still flipped this directive: the reply language was locked by a message the
	// model could not see, and the flip at position 0 invalidated the whole KV prefix mid-turn.
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

// loopAction is what the step loop does after a no-tool-call finish attempt (finishTurn): keep
// looping, because something was injected that the agent should answer, or finish the turn.
// Returning an action keeps the branch decision with the step loop that owns step/lastText, so
// finishTurn stays a pure decision.
//
// Two more used to be declared — re-enter without spending a step, and unwind a cancellation — and
// the step loop had a branch for each. finishTurn never returned either, so both were unreachable,
// and each carried a comment describing when it would happen ("re-woken by a background subagent
// result", "cancelled while parked in the bg-wait"). A reader had no way to tell that from the two
// that do fire, which is the cost: dead code is quiet, and dead code that explains itself is a
// mechanism somebody will reason about and then look for.
type loopAction int

const (
	loopContinue loopAction = iota // re-enter the step loop (feedback injected / nudged / recovered)
	loopFinish                     // the turn is done → return the result
)

// turnState is the per-turn mutable bookkeeping the step loop carries across steps and hands
// to finishTurn: the once-per-turn guards (stop hooks, empty-subagent nudge), the accounting
// behind declareAskCap, and the UNVERIFIED reason a finish carries when no council ever read
// the work.
type turnState struct {
	stopChecked bool // stop hooks enforced at most once per turn
	nudgedEmpty bool
	// spinNudged / cutNoted: the spin nudge and the cut notes are persisted user-role messages, and
	// unguarded they repeated VERBATIM on every step of a bad turn — the cut-before-acting comment's
	// own observed case was a turn that reasoned into the cap "step after step", stacking an
	// identical instruction each time. The Nth copy adds no information and dilutes the attention
	// the tool results need; once per turn says it, and the record already shows it was said.
	spinNudged      bool
	cutNoted        bool
	declareAsks     int  // how many times this turn was told to declare completion (declareAskCap)
	declareAskEpoch int  // guard.mutationEpoch() at the last such ask; a later epoch resets the count
	declared        bool // the agent declared the task finished and the council accepted
	distilAsked     bool // the finish seam already asked what was worth keeping (once per turn)
	handoffTold     bool // the turn was told once that a companion has not answered yet
	ratingAsked     bool // the turn was asked once what the answers it got were worth
	// finishTools are the tools the FINISH path itself asked for.
	//
	// Once a turn declares itself finished, its tool calls are dropped: the task is over and more
	// work on it is not wanted. But the gates that run at the finish ask for tools — rate this
	// hand-off, save that lesson — and without this those calls were dropped too. The agent did
	// exactly what it was told and nothing happened, which is the shape of a description naming a
	// way to do something there is no way to do. Observed live: rate_handoff called, no result,
	// no record.
	finishTools map[string]bool
	// dropped are the tools a declared turn tried to call and did not get to run, kept until the
	// finish path has said so. Silence here is the defect: the agent asked for something, nothing
	// happened, and the transcript keeps the call with no result — which is what a call that DID
	// happen and failed to record looks like.
	dropped          []string
	dropTold         bool
	reasks           int    // how many times this turn asked somebody again after declaring finished
	unverifiedReason string // non-empty when the turn finishes WITHOUT council approval
}

// allowAtFinish lets a tool run in the steps after a turn has declared itself done.
//
// Called by the gate that asks for it, right next to the asking, so the request and the permission
// cannot come apart — which is the whole defect: a prompt asking for a tool call, and a loop that
// throws that call away.
func (ts *turnState) allowAtFinish(names ...string) {
	if ts.finishTools == nil {
		ts.finishTools = map[string]bool{}
	}
	for _, n := range names {
		ts.finishTools[n] = true
	}
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
	// The turn's baseline read of the workspace, taken before any of its work: the finish snapshot
	// is the difference against this, so a file the turn DELETES is visible at all. Taken outside
	// the lock — it is a filesystem walk, and holding the app mutex across one would stall every
	// other session.
	worldBase := indexWorkspace(s.Workdir)
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.turnStart = runStart // what a check means by "before the step ran"
	st.worldBase = worldBase
	a.mu.Unlock()
	agentActor := event.Actor{Kind: event.ActorAgent, ID: orDefault(agent.Name, "default")}
	lastText := ""
	guard := newRunGuard()
	ts := turnState{} // per-turn mutable bookkeeping (finish guards, council accounting); zeroed field-wise on reground
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
	turnTask := "" // the user instruction THIS turn answers, snapshotted at step 0. A
	// steer that lands mid-turn is QUEUED by default (runs as its own follow-up turn), so
	// it can't silently hijack what the council judges against — unless the agent explicitly
	// routes it. A "redirect" re-snapshots turnTask (the goal changed); an "append" folds the
	// steer into turnTask, so what the council reads at the finish still carries it.
	usedTools := seedWork   // did this turn do real work? (the council skips pure conversational turns)
	handledUserPrompts := 0 // genuine (ActorUser) prompts already absorbed into turnTask; a rise past this at step>0 is a mid-turn interjection
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
	// the planner preflight, because what it clears — the todos, the interjection
	// mask, the turn notes and the retrieval caches — is what the preflight and the
	// planner then read as THIS turn's context. seedWork marks a caller that
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
	// (a redirect steer) is not instantly force-stopped by the previous goal's
	// accumulated no-progress count.
	reground := func() {
		guard.resetStall()
	}

	// No pacing ceiling — but there IS a backstop. A turn ends when the model stops calling tools,
	// when the agent declares completion and the council accepts it, when the caller's context is
	// cancelled, or when whoever launched magi stops waiting; the guards that used to force-stop a
	// run on magi's own arithmetic are gone (measured: the runs they stopped produced no pass).
	// What remains is cfg.MaxSteps (default 240) as a runaway backstop, sized far above any
	// productive turn — and when it fires at the top level, the landing below writes the honest
	// turn.finished, because falling out of this loop in silence left an open turn every reader
	// (the fleet row, UnfinishedTurnOf, a handoff's asker) read as "still working", forever.
	//
	// A workflow PHASE budget is different in kind: it declares its own budget as part of the
	// pipeline's shape (localize gets 14 steps, summarize gets 3), spending it is ordinary, and the
	// engine simply moves to the next phase. maxSteps<=0 means the caller set none.
	for step := 0; maxSteps <= 0 || step < maxSteps; step++ {
		if ctx.Err() != nil {
			return lastText, ctx.Err()
		}
		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			a.emitError(ctx, sid, agentActor, err.Error())
			return lastText, err
		}
		if step == 0 && !seeded {
			seeded = true // guard: a park-and-retry (step--) re-enters at step 0 but must not re-seed
			turnTask, handledUserPrompts = a.seedTurnTask(ctx, tc, evs)
			// Look again at whatever is still waiting, now that a turn is starting. AFTER the seed,
			// never before: re-emitting a waiting prompt ahead of it would make seedPromptIdx take
			// that one as the task and queue the real one behind it, forever.
			turnTask = a.reviewWaitingAtTurnStart(ctx, tc, turnTask)
		} else {
			// Drain any control signal a tool left last step — a routed interjection, or the
			// council tool's finish declaration — applying the reground the loop owns but the
			// tool cannot.
			ctrl := a.takeTurnControl(sid)
			// The drain empties every control field, so the declaration signal has to be caught
			// HERE or it is thrown away before the finish check ever sees it.
			if ctrl.finish {
				ts.declared = true
				if ctrl.unverifiedReason != "" {
					ts.unverifiedReason = ctrl.unverifiedReason // the rejection cap's landing, not an acceptance
				}
			}
			if tc := ctrl; tc.route != "" {
				// Absorb a routed interjection now, so it isn't also re-surfaced as its own
				// turn. The route binds to a SPECIFIC queued request (resolveRouteTarget: the
				// id the model named, else the oldest queued), not to lastUserPromptText — so
				// with several interjections piled up none is re-absorbed or cross-applied.
				// "redirect" re-anchors turnTask; "append" folds the steer in as a constraint
				// on the work already under way; "queue"/"" leaves turnTask untouched, and an
				// empty resolve means it was already absorbed, so the route is a no-op.
				if mid, it := a.resolveRouteTarget(sid, tc.routeID); it != "" {
					if nt, changed := a.applyInterjectRoute(ctx, sid, tc.route, turnTask, mid, it, reground); changed {
						turnTask = nt
					}
				}
			}
			// The second return said "reply to this interjection instead of parking"; there is no
			// park left to skip, so only the handled count is carried.
			handledUserPrompts, _ = a.detectInterjections(ctx, tc, evs, turnTask, handledUserPrompts)
		}
		// The council tool may run inside this step; give it the task the loop is actually
		// answering (a redirect re-anchors it) rather than letting it recompute from a transcript
		// view that hides the redirect and falls back to the abandoned original.
		a.setLiveTurnTask(sid, turnTask)

		// One model response for this step, with the two recoveries that belong to getting it:
		// a context too large to send, and a backend that goes silent (see generateStep).
		sr, gerr := a.generateStep(ctx, tc, agent, agentActor, evs, step, cumOut)
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
			if !ts.spinNudged {
				ts.spinNudged = true
				_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"}, reasoningSpinNudge)
			}
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

		// The provider ended that reply at the output-token cap. Say so, now that the prefix it did
		// send is on the record, so the next step is not written on the assumption the model said
		// everything it meant to.
		if res.finishReason == "length" {
			note := cutByOutputCapNote
			if len(toolCalls) == 0 {
				note = cutBeforeActingNote // spent the budget without acting: the fix is a tool, not more text
			}
			if !ts.cutNoted {
				ts.cutNoted = true
				_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"}, note)
			}
		} else if res.cut {
			// Same shape, different cause: the connection ended mid-reply. Say which one it was,
			// on the record with the prefix, and keep going — the run used to end here.
			if !ts.cutNoted {
				ts.cutNoted = true
				_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"}, cutByLostStreamNote)
			}
		}

		// No tool calls → the turn wants to finish. Stop hooks enforce checks
		// (e.g. tests must pass); a failure pushes the agent to keep working.
		// The agent declared the task finished and the council accepted, so this turn is over — but
		// it ends the way every other turn ends. Returning from here directly would skip the finish
		// path itself: no turn.finished, no finalize stage, and a steer that landed during the
		// declaration left stranded instead of picked up as its own turn.
		if a.finishDeclared(&ts, sid) {
			toolCalls = a.callsAfterDeclaring(ctx, sid, toolCalls, &ts)
		}
		if len(toolCalls) == 0 {
			// Turn-cumulative usage (§8.1): out/cost summed across steps, in = last.
			u := turnUsage(a, sid, usageAtStart, lastIn, cumOut, cumCost)
			switch a.finishTurn(ctx, tc, step, turnTask, lastText, evs, usedTools, handledUserPrompts, u, &ts) {
			case loopContinue:
				continue // feedback injected / nudged / stuck-recovered — keep working
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
			// The turn can be cut while a batch is being launched, and the sequential branch below
			// checks for it between calls. This one did not: an interrupted turn still started
			// every remaining call in the batch, and each of them then discovered the cancelled
			// context for itself. Checked here so the two branches stop for the same reason.
			var wg sync.WaitGroup
			for _, tc := range toolCalls {
				if ctx.Err() != nil {
					break
				}
				wg.Add(1)
				go func(tc *session.ToolCall) {
					defer wg.Done()
					a.executeTool(ctx, s, agent, depth, agentActor, tc, guard, turnTask)
				}(tc)
			}
			wg.Wait()
			if ctx.Err() != nil {
				return lastText, ctx.Err()
			}
		} else {
			for _, tc := range toolCalls {
				if ctx.Err() != nil {
					return lastText, ctx.Err()
				}
				a.executeTool(ctx, s, agent, depth, agentActor, tc, guard, turnTask)
			}
		}

		// Corrective re-grounding: before any force-stop, give a thrashing agent ONE nudge to
		// re-read the task and change approach — far cheaper than burning the rest of the budget.
		a.injectStuckNudge(ctx, tc, turnTask, evs)
	}
	// The step budget is spent. For a workflow phase and for a child that is ordinary: the phase's
	// engine moves on, and a child's result is read by the tool call that spawned it. A TOP-LEVEL
	// turn reaching here hit the runaway backstop, and it must not end in silence: nothing after
	// this writes a persisted turn.finished (the run goroutine's teardown covers only cancels and
	// errors), so without this the log kept an open turn that the fleet row, UnfinishedTurnOf and a
	// handoff's asker all read as "still working" — forever. The work stands as it was left; the
	// finish says so, UNVERIFIED, the same honest landing an undeclared turn gets.
	if depth == 0 && !a.cfg.Workflow {
		d, _ := json.Marshal(event.TurnFinishedData{
			Usage:      turnUsage(a, sid, usageAtStart, lastIn, cumOut, cumCost),
			Unverified: true,
			Reason: fmt.Sprintf("the turn spent the %d-step runaway backstop without finishing — "+
				"the work stands as it was left", maxSteps),
		})
		a.appendFact(ctx, sid, event.TypeTurnFinished, agentActor, d)
	}
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
		if seed := seedPromptIdx(evs, a.deferredInterjectIDs(sid)); seed >= 0 && seed < len(entries) {
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
// so it can't swap what the council judges against.
//
// There is one handling now: queue it to run as its own turn, and tell the agent it is deferred so
// it stops oscillating (it may still call route_interjection to redirect or append). The two other
// branches this used to have both existed because the agent could be waiting on subagents — one
// for an orchestrator idle-parked on its own explorers, one for ordinary background delegation.
// Nothing dispatches, so nothing parks, and a single path is what is left.
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
// compaction, and the reconstructed message history with the per-step-volatile context
// (live plan/experience/RAG) appended as an ephemeral trailing user message — never
// persisted, so it stays out of the event log, language lock, and council snapshot.
// Compaction can inject events and re-read the log, so the possibly-refreshed evs is
// returned alongside the request. Also publishes the live context-usage meter.
func (a *App) buildStepRequest(ctx context.Context, tc turnCtx, evs []event.Event, step, cumOut int) (port.ChatRequest, []event.Event) {
	s, agent, agentActor := tc.s, tc.agent, tc.actor
	sid := s.ID
	// The MASKED view, so the language lock (and anything else the system prompt derives from
	// events) sees exactly what the messages will show — see buildStepSystem.
	sys := a.buildStepSystem(agent, s.Workdir, a.liveEvents(sid, evs))

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
		// What a recall re-hydrated may have just been shed again, so "already recalled this topic
		// this turn — use what was returned earlier" would point at content that is no longer here.
		tc.guard.forgetRecalledTopics()
		evs, _ = a.store.Read(ctx, sid, 0)
		raw = reconstruct(evs) // refresh after compaction
		vol = withNote(a.volatileContext(ctx, s, agent, evs, raw, step, tc.maxSteps, time.Since(tc.runStart)))
	}

	msgs := collapseRepeatedCalls(reconstruct(a.liveEvents(sid, evs)))
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
			// An error, so the agent re-runs it in a narrower form — and advisory, because the
			// sentence above says it outright: the call RAN. What is missing is the record of what
			// it answered, not the work; a screen drawing ✗ over a write that landed would be
			// contradicting the filesystem, which is the same defect the diagnostics path had.
			CallID: part.ToolResult.CallID, Content: c, IsError: true, Advisory: true,
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
		//
		// dangerGated rather than the raw map, so an MCP tool is serialized too: it can
		// prompt (there is one modal slot), and what it does on its server is not
		// something magi can prove read-only.
		if fileModifiers[tc.Name] || a.dangerGated(tc.Name) {
			return false
		}
		// A subagent runs a whole child turn, which writes files under the PARENT's guard — and the
		// guard's before/after capture assumes writes to a file are serialised.
		//
		// Unless the children cannot collide. A tool that declares ReadOnlyChildren has every spawn
		// checked against that claim at the moment it is made (see spawnFnFor), so two of them have
		// nothing to race over — and running them one after the other is two whole child turns of
		// wall clock for work that shares nothing. IsolatedChildren reaches the same safety the
		// other way: its writing children each get their own checkout (the default is applied where
		// the workspace is decided), so there is still no shared tree for the guard to worry about.
		if a.tools != nil {
			if t, ok := a.tools.Get(tc.Name); ok {
				m := port.ToolMetaOf(t)
				if m.Subagent && !m.ReadOnlyChildren && !m.IsolatedChildren {
					return false
				}
			}
		}
		// A tool that blocks on the PERSON is not parallel-safe however read-only it is: there is
		// one human and one modal slot, so a second question raised while the first is up replaces
		// it on screen — the first prompt vanishes and its call waits on an answer nobody can give,
		// with nothing saying it is there. Serializing is the whole fix: the second never starts
		// until the first has been answered.
		if tc.Name == "ask_user" {
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
		In:     now.In - start.In,
		Out:    now.Out - start.Out,
		Cost:   now.Cost - start.Cost,
		Cached: now.Cached - start.Cached,
		// Reported if it was reported at any point in this turn. A backend that answers some
		// requests with a details block and some without is not a backend that stopped having a
		// cache; treating one silent response as "unknown" would blink the reading in and out.
		CacheReported: now.CacheReported,
	}
	if u.In <= 0 && u.Out <= 0 {
		return event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}
	}
	if u.Cost <= 0 {
		u.Cost = cumCost
	}
	return u
}
