package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
func (a *App) buildStepSystem(sid session.SessionID, agent AgentSpec, workdir string, evs []event.Event) string {
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
	// Available skills (model loads one via the skill tool when relevant). FROZEN per session —
	// see skillBlock, and skillArrivals for what happens to one that shows up later.
	sys += a.skillBlockFor(sid, workdir)
	return sys
}

// skillBlockFor renders the skill list ONCE per session and hands back the same bytes every time
// after that.
//
// The block rides at the head of every request. Re-rendering it when the directory changes moves
// position 0, and everything after position 0 stops being a cache hit — the same failure the
// language directive above documents, for a list that changes far more often than a language does.
// It changes from several directions, none of them rare: engram saving what it just learned, the
// agent writing a skill because the user asked for one, the user dropping a file in by hand.
//
// So the head keeps what it opened with. What arrives later is announced instead (skillArrivals),
// which costs one line and leaves the prefix whole. Nothing is hidden: the model reads the frozen
// list plus the notes, and the next session's prompt merges them at no cost.
func (a *App) skillBlockFor(sid session.SessionID, workdir string) string {
	sk := a.loadSkills(workdir)
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	if st.skillBlockSet {
		return st.skillBlock
	}
	st.skillSeen = map[string]bool{}
	for _, x := range sk {
		st.skillSeen[x.Name] = true
	}
	st.skillBlock = renderSkillBlock(sk)
	st.skillBlockSet = true
	return st.skillBlock
}

// renderSkillBlock is the skill list as the head carries it — a pure function of the list, so a
// reading that wants to know its size (assembledParts) can render it without freezing anything.
func renderSkillBlock(sk []port.Skill) string {
	var b strings.Builder
	for _, x := range sk {
		b.WriteString("- " + x.Name + ": " + oneLineHint(x.Description) + "\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\n# Available skills (use the skill tool to load one)\n" + strings.TrimRight(b.String(), "\n")
}

// skillArrivals names skills that appeared after this session's head was written, once each, and
// records them as told. The caller appends the note to the transcript rather than editing the head.
func (a *App) skillArrivals(sid session.SessionID, workdir string) []string {
	sk := a.loadSkills(workdir)
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	if !st.skillBlockSet {
		return nil // the head has not been written yet; it will carry these
	}
	if st.skillSeen == nil {
		st.skillSeen = map[string]bool{}
	}
	var out []string
	for _, x := range sk {
		if st.skillSeen[x.Name] {
			continue
		}
		st.skillSeen[x.Name] = true
		out = append(out, "- "+x.Name+": "+oneLineHint(x.Description))
	}
	return out
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
	// spins counts reasoning-only spins this turn, and it is what the nudge escalates on.
	//
	// This was a bool: nudge once, then say nothing. The reason was sound — an identical
	// instruction stacked on every step adds no information and dilutes the attention the tool
	// results need — but the conclusion was wrong. Measured on schemelike-metacircular-eval
	// (2026-08-19): NINE spins, ten minutes apart, and only the first carried a word to the model.
	// The other eight were cancelled in silence, so the model re-derived from scratch each time
	// with nothing telling it why its answer kept vanishing. One eighty-minute hang became eight
	// ten-minute hangs and 82 minutes passed with no tool call.
	//
	// So the rule is not "say it once", it is "never say the same thing twice". Each repeat names
	// the repetition and narrows what is being asked for, which is information the previous one
	// did not carry.
	spins int
	// malformed counts replies shaped like a tool call that named no tool; each gets a repair
	// request (F-LLM-FALLBACK R3), at most two a turn — then such a reply is shown as text.
	malformed       int
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
	// held is the last step's own usage — what the window holds now — recorded on the finish as
	// TurnFinishedData.Held beside the bill.
	held event.Usage
	// lastStep is the previous step's signature (text + calls) and lastCalls its call ids — the
	// identical-step check compares against them.
	lastStep      string
	lastCalls     []string
	lastPromptSeq int64
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
	// What earlier turns held on to is swept HERE, at the start of the next turn, and not when a
	// turn lands. The moment somebody is most likely to want a file back is just after the turn
	// that replaced it — exactly when a sweep at that turn's end would take it — and a sweep at
	// the end also never runs for the turns most likely to have made a mess, the ones that end by
	// error or cancellation rather than by landing.
	if depth == 0 {
		sweepTrash(s.Workdir)
	}
	worldBase := indexWorkspace(s.Workdir)
	// Only when a council can convene: the disclaimer has exactly one reader, and a probe run
	// for a consumer that does not exist is a git status per turn nobody reads.
	var preDirt []string
	if a.cfg.Council != nil {
		preDirt = a.dirtyBeforeTurn(ctx, s.Workdir)
	}
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.turnStart = runStart // what a check means by "before the step ran"
	// The one sanctioned moment the head may change: the turn is new, and the prefix beyond its
	// user message is not written yet. See prompt_frozen.go.
	st.turnSys, st.turnSysSet = "", false
	st.worldBase = worldBase
	if st.arrival == nil {
		st.arrival = worldBase // the first turn's reading, kept for the life of the session
	}
	st.preexistingDirt = preDirt
	a.mu.Unlock()
	agentActor := event.Actor{Kind: event.ActorAgent, ID: orDefault(agent.Name, "default")}
	lastText := ""
	guard := newRunGuard(a.touchesFile)
	ts := turnState{} // per-turn mutable bookkeeping (finish guards, council accounting); lives for the whole turn — reground resets only the guard's stall arm (see reground)
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
			turnTask, handledUserPrompts = a.applyToolControl(ctx, tc, &ts, evs, turnTask,
				handledUserPrompts, reground)
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
			ts.spins++
			a.emitToolProgress(sid, agentActor, "", agent.Name,
				fmt.Sprintf("cancelled a reasoning-only spin (%d) — take a concrete action", ts.spins))
			// Every spin gets a word, and no two words are the same. emitToolProgress above is a
			// live progress line for a person watching; only this reaches the model.
			//
			// The tail of what was cancelled goes back WITH the nudge. The response itself is
			// discarded — it is incomplete and ends mid-thought — but discarding it silently made
			// the instruction impossible to follow: "act on what you worked out" named something
			// that was no longer anywhere in the model's context, so it re-derived instead, which
			// is the loop this guard exists to break. Measured on regex-log (2026-08-19): a
			// correct, nearly complete regex design was cancelled and thrown away, and the next
			// response began the same analysis again from the top.
			say := reasoningSpinNudge(ts.spins)
			if tail := salvageTail(res.text, res.reasoning); tail != "" {
				say += "\n\nHere is the END of what you had worked out before it was cut off. It is " +
					"all that survives — the rest is gone. Continue FROM this instead of deriving " +
					"it again:\n\n" + tail
			}
			_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"}, say)
			continue
		}
		// A reply shaped like a tool call that could not be read as one — no tool name (gpt-oss via
		// Ollama drops it). Nothing ran, and showing the JSON as prose tells the person nothing (Excel
		// 2021, 2026-09-07: {"address":"A1","text":"…"} on the screen, no note on A1). Ask the model to
		// say it again as a real call (F-LLM-FALLBACK R3), twice at most; the reply is discarded like a
		// spin, and travels back inside the request so it can be corrected rather than re-derived.
		if res.malformedCall && len(res.toolCalls) == 0 && ts.malformed < 2 {
			ts.malformed++
			// The reply is discarded but it was generated and metered — unlike a spin (cancelled
			// mid-stream, usage rarely arrives) this one finished, so its tokens go on the bill.
			if res.usage != nil {
				cumOut += res.usage.Out
				if res.usage.In > 0 {
					lastIn = res.usage.In
					ts.held = *res.usage
				}
			}
			a.emitToolProgress(sid, agentActor, "", agent.Name,
				fmt.Sprintf("a reply looked like a tool call but named no tool (%d) — asking for a real call", ts.malformed))
			_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"}, malformedCallNudge(ts.malformed, res.text))
			continue
		}
		text, reasoning := res.text, res.reasoning
		toolCalls, usage, textConsumed := res.toolCalls, res.usage, res.textConsumed
		// Accumulate this step's usage into the turn totals (§8.1).
		if usage != nil {
			cumOut += usage.Out
			if usage.In > 0 {
				lastIn = usage.In
				// What the window holds after THIS step — this request and its answer — for the
				// finish to record beside the bill (TurnFinishedData.Held).
				ts.held = *usage
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
		// **An exact repeat of the previous step asks for nothing new.** The same text and the same
		// calls with the same arguments, right after a step whose calls all succeeded: running them
		// again returns what they returned a moment ago. Measured live (Excel, 2026-09-07): after
		// `land` answered "you may end here", the model repeated "text + land" seven times, byte
		// for byte, until the repeat nudge fired — seven landings on the screen and seven model
		// calls billed for one sentence. A turn that has nothing new to say is finished; the note
		// says so on the record, and the calls are not run again. A repeat after a FAILURE is a
		// retry and runs as before.
		//
		// Two things make a repeat NOT churn, and both are checked: something new arrived between
		// the steps (a nudge, an interjection, a gate's question, a hand-off's answer — every one
		// of those is a prompt event, so the newest prompt seq says whether the model was told
		// anything), or the call is one whose repeat has its own meaning — a council declaration
		// is judged, not counted; a wait or a poll is meant to be asked again; a question to a
		// person or a companion has its own cap.
		promptSeq := newestPromptSeq(evs)
		if sig := stepSignature(text, toolCalls); repeatIsChurn(&ts, sig, toolCalls, promptSeq, guard) {
			if err := a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "loop"}, repeatedStepNote); err != nil {
				// Best-effort: the note explains the ending to whoever reads the log; the ending
				// itself does not depend on it, and a store that cannot take one line now will
				// refuse the turn.finished a moment later, where the error is not swallowed.
				log.Printf("magi: could not record why the turn ended on a repeated step: %v", err)
			}
			text, toolCalls = "", nil
		} else {
			ts.lastStep, ts.lastCalls, ts.lastPromptSeq = sig, callIDsOf(toolCalls), promptSeq
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
				// The turn is over — but "over" is not "accepted", and the plan must not be
				// resolved as though it were. An UNVERIFIED landing is the cap's, not a
				// council's: nobody judged the task satisfied, so every unfinished step
				// resolves the way it does on any other non-acceptance (cancelled), showing
				// what was left undone. Folding both into one true wrote "completed" across a
				// plan whose first step never ran — the event right after a turn.finished that
				// says unverified, contradicting it.
				finished = ts.unverifiedReason == ""
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

		// **A tool ended the turn.** The landing plugin's `land` says so through magi.finish; when
		// the model has already written its answer (the text before the call), the turn finishes
		// here — the same finish path a call-less step takes. With no text yet, the mark lapses and
		// the next step ends the turn the ordinary way, carrying the answer.
		if a.takeFinishNow(sid) && strings.TrimSpace(lastText) != "" {
			u := turnUsage(a, sid, usageAtStart, lastIn, cumOut, cumCost)
			switch a.finishTurn(ctx, tc, step, turnTask, lastText, evs, usedTools, handledUserPrompts, u, &ts) {
			case loopContinue:
				continue
			case loopFinish:
				finished = ts.unverifiedReason == ""
				return lastText, nil
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
			Held:       heldOf(ts.held),
			Prompt:     shapeOf(a, sid),
			Unverified: true,
			Reason: fmt.Sprintf("the turn spent the %d-step runaway backstop without finishing — "+
				"the work stands as it was left", maxSteps),
		})
		a.appendFact(ctx, sid, event.TypeTurnFinished, agentActor, d)
	}
	return lastText, nil
}

// applyToolControl drains the signal a tool left on the previous step and folds it into what this
// step is answering. Beside generateStep, which is the same idea: the loop keeps the sequence, the
// phases get names.
//
// Two things arrive this way, and neither is something a tool can do for itself:
//
//   - the council tool's finish declaration, which ends the turn. The drain empties every control
//     field, so it has to be caught HERE or it is thrown away before the finish check sees it.
//   - a routed interjection, which re-anchors or constrains the task. It binds to a SPECIFIC queued
//     request rather than to the last user prompt, so several piled-up interjections are neither
//     re-absorbed nor cross-applied.
//
// Returns the task this step should answer and the count of user prompts already absorbed; ts is
// written through, because the declaration is turn state and not step state.
func (a *App) applyToolControl(ctx context.Context, tc turnCtx, ts *turnState, evs []event.Event,
	turnTask string, handledUserPrompts int, reground func()) (string, int) {
	sid := tc.s.ID
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
	return turnTask, handledUserPrompts
}

// highestSeq is the last event a slice carries — the high-water mark of what a step has seen.
func highestSeq(evs []event.Event) int64 {
	var max int64
	for _, e := range evs {
		if e.Seq > max {
			max = e.Seq
		}
	}
	return max
}

// rereadWithoutUnscannedSteers re-reads the log for a step that has already had its interjection
// scan, and leaves behind anything the person typed since.
//
// A step masks a mid-turn steer by first ENQUEUEING it: the top-of-loop scan puts it in
// pendingInterject, and liveEvents drops exactly what is in that queue. So the mask only covers
// prompts the scan actually saw. A step then re-reads the log twice more — once to pick up the
// arrival notes it just appended, once after compaction — and both of those reads can bring back
// a steer that landed in between. Never scanned, so never queued; never queued, so never masked;
// and the request goes out carrying BOTH the task the turn is on and the message the person typed
// into the middle of it, with nothing to say they are not one request.
//
// That is a model asked to answer two things at once, told nothing about the difference — and
// answering both, or answering the same thing twice for two readings of one prompt, is what that
// looks like from outside. Reported as the agent repeating itself, which is the honest description
// of the symptom.
//
// The rule this restores is simple and was always the intent: a step answers the world as of its
// own scan. A steer that arrives after it belongs to the NEXT step's scan, which will queue it,
// mask it, and give it its own turn — the finish boundary catches even the last one
// (enqueueLateInterjections). Nothing is dropped here; it is deferred to the reader that owns it.
//
// Only user prompts are held back. Everything else the re-read brings — the arrival notes, a
// compaction's rewritten history, tool results — is the step's own business and must land.
func (a *App) rereadWithoutUnscannedSteers(ctx context.Context, sid session.SessionID, scanned int64) []event.Event {
	fresh, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil
	}
	out := make([]event.Event, 0, len(fresh))
	for _, e := range fresh {
		if e.Seq > scanned && e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			continue
		}
		out = append(out, e)
	}
	return out
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
	// A skill that showed up after the head was written is APPENDED, never folded back into it.
	// Emitted before the messages are rebuilt so it lands in this step's request rather than the
	// next one, and it goes in as a system-actor prompt — the ⟳ note pattern, which the model
	// reads and a person can see, and which never counts as an unanswered user turn.
	// What this step's interjection scan has already seen. Every re-read below is filtered
	// against it — see rereadWithoutUnscannedSteers.
	scanned := highestSeq(evs)
	arrivals := make([]string, 0, 4)
	for _, line := range a.skillArrivals(sid, s.Workdir) {
		arrivals = append(arrivals,
			"A skill became available since this conversation started (use the skill tool to load it):\n"+line)
	}
	// Memories arriving by a side door get the same treatment, capped per step so a busy fleet
	// feeding the shared store cannot turn a step into a bulletin board.
	for _, line := range a.takeMemoryArrivals(sid, 3) {
		arrivals = append(arrivals,
			"A team memory matching this task became available (use recall_memory to read it):\n"+line)
	}
	for _, text := range arrivals {
		pd, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: "m_" + newID(),
			Parts:     []session.Part{{Kind: session.PartText, Text: text}},
		})
		if err := a.appendFact(ctx, sid, event.TypePromptSubmitted,
			event.Actor{Kind: event.ActorSystem, ID: "arrivals"}, pd); err == nil {
			evs = a.rereadWithoutUnscannedSteers(ctx, sid, scanned)
		}
	}
	sys := a.stepSystemFor(sid, agent, s.Workdir, a.liveEvents(sid, evs))

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
		evs = a.rereadWithoutUnscannedSteers(ctx, sid, scanned)
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
	specs := a.sessionToolSpecs(sid, agent)
	a.notePromptShape(sid, s.Model.Model, sys, msgs, specs)
	a.publishContextUsage(sid, agentActor, s.Model.Model, sys, msgs, specs, cumOut)

	return port.ChatRequest{
		Model:    s.Model.Model,
		System:   sys,
		Messages: msgs,
		Tools:    specs,
	}, evs
}

// publishContextUsage emits a live context meter for the UI (M6/context mgmt).
// outTokens is the turn's cumulative output so far, for the live ↓ readout (§8.1).
func (a *App) publishContextUsage(sid session.SessionID, actor event.Actor, modelID, sys string,
	msgs []session.Message, specs []port.ToolSpec, outTokens int) {
	window := a.contextWindow(modelID)
	// The tool catalog rides on every request and was not in this meter, so the live readout ran
	// 6-7k tokens light on the default roster — the IDE reads this event and nothing else, so its
	// gauge was the one still missing a piece after the console and the terminal were fixed. The
	// compaction trigger has counted the catalog since it was measured; this is the same sum.
	tokens := a.contextTokens(sid, sys, msgs)
	if est := estimateTokens(sys, msgs) + toolSpecTokens(specs); est > tokens {
		tokens = est
	}
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
	a.emitErrorKind(ctx, sid, actor, msg, false)
}

// emitErrorKind records a provider/stream failure, saying whether the RUN kept going past it.
// Every site carries the machine code so the headless contract ("error[<code>]: …" on stderr)
// holds; `recovered` is the separate question of whether the turn is over, which a reader must
// not infer from the presence of an error event alone.
func (a *App) emitErrorKind(ctx context.Context, sid session.SessionID, actor event.Actor, msg string, recovered bool) {
	d, _ := json.Marshal(event.ErrorData{Message: msg, Code: "provider", Recovered: recovered})
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
		if a.changesFile(tc.Name) || a.dangerGated(tc.Name) {
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

// repeatedStepNote is what the record says when a step was an exact repeat of the one before it.
const repeatedStepNote = "That step repeated the previous one exactly — the same words and the same calls — " +
	"and those calls all succeeded a moment ago, so nothing new was asked for. The calls were not run " +
	"again and the turn ends here."

// stepSignature is the step as the model wrote it: its text and every call's name and arguments.
// Empty for a step with no calls — a text-only step is a finish, not something to compare.
func stepSignature(text string, calls []*session.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(text))
	for _, c := range calls {
		if c == nil {
			continue
		}
		b.WriteString("\x00")
		b.WriteString(c.Name)
		b.WriteString("\x00")
		b.Write(c.Args)
	}
	return b.String()
}

// repeatExempt are the tools whose identical repeat means something: a declaration is for the
// council to judge, a wait or a poll is asked again by design, and a question to a person or to a
// companion is capped by its own rules.
var repeatExempt = map[string]bool{"council": true, "wait_for": true, "bash_output": true, "ask_user": true, "hand_off": true}

// repeatIsChurn is whether this step is an exact repeat of the previous one with nothing new in
// between: the same signature, no prompt event since (promptSeq unchanged), every previous call
// succeeded, and no call whose repeat has its own meaning.
func repeatIsChurn(ts *turnState, sig string, calls []*session.ToolCall, promptSeq int64, guard *runGuard) bool {
	if sig == "" || len(calls) == 0 || sig != ts.lastStep || promptSeq != ts.lastPromptSeq {
		return false
	}
	for _, c := range calls {
		if c != nil && repeatExempt[c.Name] {
			return false
		}
	}
	return guard == nil || !guard.anyFailed(ts.lastCalls)
}

// newestPromptSeq is the seq of the newest prompt event in the log — what "was the model told
// anything since" is measured by.
func newestPromptSeq(evs []event.Event) int64 {
	var out int64
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == event.TypePromptSubmitted {
			return evs[i].Seq
		}
	}
	return out
}

func callIDsOf(calls []*session.ToolCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if c != nil {
			out = append(out, c.CallID)
		}
	}
	return out
}

// heldOf is the Held a finish records: the last step's own usage, or nothing when no step reported
// one — a zero would read as "an empty window", which is not what silence means.
func heldOf(u event.Usage) *event.Usage {
	if u.In <= 0 {
		return nil
	}
	h := u
	return &h
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

// errorRecovered reports whether an error event is one the run kept working past. Readers that
// treat an error as a turn boundary (the fork split, the unfinished-turn scan, the outcome the
// observer sees) must ask this rather than the event's presence: since the loop learned to carry
// on past a cut or guard-aborted stream, "an error happened" and "the turn ended" are no longer
// the same statement.
func errorRecovered(e event.Event) bool {
	var d event.ErrorData
	return json.Unmarshal(e.Data, &d) == nil && d.Recovered
}
