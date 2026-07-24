package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Mid-turn interjection / steer machinery, split out of loop.go: routing a user
// message that arrives while a turn runs (applyRoute / noteInterjection), the
// idle-park and finish-boundary triage mini-turns (handleAside / triageQueued /
// interjectTurn / execAsideTool), agent-initiated replan (honorReplan), and late-steer
// enqueue at the finish boundary. Behavior unchanged; runLoop/finishTurn stay in loop.go.

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

// applyInterjectRoute absorbs a routed mid-turn interjection into the running turn and
// applies the reground the loop owns. It returns the (possibly re-anchored) turnTask and
// whether it changed. The reground COST differs by route, and that difference is the whole
// point of the fix:
//   - "redirect": the goal itself changed, so reground(true) — a fresh decomposition and a
//     new plan audit are warranted.
//   - "append": the approved plan is FROZEN for the turn. The steer is injected as a
//     constraint on the in-progress work (injectSteerConstraint) and reground(false) resets
//     only the stall/council accounting — NO re-plan, NO re-audit, NO explorer re-dispatch.
//     The steer is still enforced at completion because turnTask now folds it in, so the
//     termination council judges against original+steer.
//   - "queue"/"" : nothing changed; the interjection stays queued to run as its own turn.
//
// msgID identifies the specific queued interjection being absorbed; it is consumed by id so
// re-draining the same signal is a no-op (idempotency) even when two interjections share text.
func (a *App) applyInterjectRoute(ctx context.Context, sid session.SessionID, route, turnTask, msgID, interject string, reground func(bool)) (newTask string, changed bool) {
	nt, changed := applyRoute(route, turnTask, interject)
	if !changed {
		return turnTask, false
	}
	a.consumeInterjectByID(ctx, sid, msgID)
	if route == "redirect" {
		reground(true)
	} else {
		a.injectSteerConstraint(ctx, sid, interject)
		reground(false)
	}
	return nt, true
}

// noteInterjection tells the agent a new user message arrived mid-turn. When
// deferred (not dispatching) it has been QUEUED to run after the current task, so the
// agent keeps focus instead of oscillating between the two (the live-observed thrash:
// plexus #7–#10) and may call route_interjection to redirect/append when confident.
// When dispatching (background subagents running, agent otherwise idle) the message is
// left visible and the agent is invited to answer it briefly without abandoning the task.
//
// The notice is EPHEMERAL: it is staged in session state and injected into the per-step
// volatile context (buildStepRequest), never persisted as a PromptSubmitted fact. A
// persisted notice outlived its interjection — every later turn (and a session reload)
// still carried a stale "queued" note with a copy of the prompt, so an already-resolved
// interjection could influence the next request. The queued-case note is keyed by the
// origin MessageID and vanishes the moment the interjection resolves; the dispatch-case
// nudge is one-shot (consumed by the next step's request).
func (a *App) noteInterjection(sid session.SessionID, turnTask, msgID, interject string, dispatching bool) {
	reqLine := ""
	if h := shortReqID(msgID); h != "" {
		reqLine = "\n\nThis request's id is [req: " + h + "] — pass it as route_interjection request_id to route THIS one."
	}
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
	text += reqLine
	a.mu.Lock()
	st := a.stateLocked(sid)
	if dispatching {
		st.asideNoteOnce = text
	} else {
		if st.interjectNotes == nil {
			st.interjectNotes = map[string]string{}
		}
		st.interjectNotes[msgID] = text
	}
	a.mu.Unlock()
}

// takeInterjectNotes assembles the interjection notices to append to THIS step's
// volatile context: every note whose queued interjection is still unresolved (pruning
// the rest — a note must not outlive its interjection), plus the one-shot dispatch
// nudge, which is consumed here. Empty when there is nothing pending.
func (a *App) takeInterjectNotes(sid session.SessionID) string {
	deferred := a.deferredInterjectIDs(sid)
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return ""
	}
	var parts []string
	if len(st.interjectNotes) > 0 {
		// Prune resolved notes; emit the live ones in a stable (msgID) order.
		ids := make([]string, 0, len(st.interjectNotes))
		for id := range st.interjectNotes {
			if deferred == nil || !deferred[id] {
				delete(st.interjectNotes, id) // resolved (routed/drained/resurfaced) → gone
				continue
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			parts = append(parts, st.interjectNotes[id])
		}
	}
	if st.asideNoteOnce != "" {
		parts = append(parts, st.asideNoteOnce)
		st.asideNoteOnce = ""
	}
	return strings.Join(parts, "\n\n")
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
func (a *App) handleAside(ctx context.Context, agent AgentSpec, s session.Session, depth int, turnTask, msgID, aside string) (acted bool) {
	sys := asideHandlerSystem
	if h := shortReqID(msgID); h != "" {
		sys += "\n\nThis message's request id is [req: " + h + "] — pass it as route_interjection request_id to route THIS message."
	}
	if t := strings.TrimSpace(turnTask); t != "" {
		sys += "\n\nThe background task (context only — do not act on it here):\n" + clipSpec(t, 500)
	}
	replied, eff := a.interjectTurn(ctx, agent, s, depth, sys, aside, modeAside, msgID)
	switch {
	case eff.didRoute:
		return true // redirect/append: the loop's turnControl drain consumes + re-anchors next step
	case eff.didCancel:
		a.consumeInterject(ctx, s.ID, aside) // cancel with no re-anchor: resolved here
		return true
	case replied:
		a.consumeInterject(ctx, s.ID, aside) // chitchat: answered here, don't also re-run as a turn
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
func (a *App) triageQueued(ctx context.Context, agent AgentSpec, s session.Session, msgID, aside string) (escalate bool) {
	sys := queuedTriageSystem
	if tail := a.recentTranscript(ctx, s.ID, 8, 2000); tail != "" {
		sys += "\n\nRecent conversation (for context — do not re-answer it):\n" + tail
	}
	replied, eff := a.interjectTurn(ctx, agent, s, 0, sys, aside, modeQueued, msgID)
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
func (a *App) interjectTurn(ctx context.Context, agent AgentSpec, s session.Session, depth int, sys, aside string, mode triageMode, replyTo string) (replied bool, eff asideEffect) {
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
			// Tag the visible reply with the answered message's origin id (replyTo) so the TUI
			// can pull that question bubble down into a [question → answer] group — but ONLY for a
			// PURE inline answer with no side effect. If the interjection acted on the task —
			// routed (modeAside re-anchor: didRoute; modeQueued: escalate), or cancelled a subagent
			// (didCancel) — its visible text is an ack for a real action woven into the main flow,
			// not a standalone answer; grouping it would detach the question bubble from the steer
			// it actually applied (or, for modeQueued, double-move it against the resurface). The
			// effect flags are sticky across the mini-loop's steps, so a confirmation emitted in a
			// later tool-call-free step (calls==0) still reads its earlier route/cancel and stays
			// untagged; len(calls)>0 covers the same-step ack, before execAsideTool sets the flags.
			rt := replyTo
			if len(calls) > 0 || eff.escalate || eff.didRoute || eff.didCancel {
				rt = ""
			}
			a.appendReplyPart(ctx, s.ID, actor, msgID, rt, session.RoleAssistant, session.Part{ID: textPartID, Kind: session.PartText, Text: reply})
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
		RouteInterjection: func(action, reason, requestID string) error {
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
				tc.routeID = requestID
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
// It returns true when the replan was actually honored (plan rebuilt, accounting reset), so the
// caller can record the replan against the sterile-replan convergence counter.
func (a *App) honorReplan(ctx context.Context, sid session.SessionID, reason string, count, atCalls *int, curCalls int, reground func(bool)) bool {
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
		return false
	}
	// Require real tool work between replans. guard.callCount() counts EVERY tool call,
	// including the replan call that raised this signal, so a back-to-back replan-only step
	// still advances curCalls by exactly 1 (its own call) over the last honored replan's
	// snapshot. Anything at-or-below that +1 means nothing but the replan itself happened —
	// churn — so refuse; genuine work (bash/edit/read) lands curCalls at atCalls+2 or more.
	if *atCalls >= 0 && curCalls <= *atCalls+1 {
		inject("Replan refused: you replanned again without taking any real action since the last replan. Actually " +
			"attempt the current plan (run a command, edit a file, inspect why it failed) before deciding it is unworkable.")
		return false
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
	return true
}

// ---- interjection / steer state accessors (moved from app.go) ----
// State-management layer for the mid-turn steer machinery above: turnControl signals,
// the pending-interjection queue, and the interjection-seen mask. All guard a.mu.
