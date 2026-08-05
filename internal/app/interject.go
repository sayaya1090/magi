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
// the finish-boundary triage mini-turn (triageQueued /
// interjectTurn / execAsideTool) and late-steer
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
//   - "redirect": the goal itself changed, so reground() — the stall accounting starts over.
//   - "append": the approved plan is FROZEN for the turn. The steer is injected as a
//     constraint on the in-progress work (injectSteerConstraint) and reground() resets
//     only the stall/council accounting — NO re-plan, NO re-audit, NO explorer re-dispatch.
//     The steer is still enforced at completion because turnTask now folds it in, so the
//     termination council judges against original+steer.
//   - "queue"/"" : nothing changed; the interjection stays queued to run as its own turn.
//
// msgID identifies the specific queued interjection being absorbed; it is consumed by id so
// re-draining the same signal is a no-op (idempotency) even when two interjections share text.
func (a *App) applyInterjectRoute(ctx context.Context, sid session.SessionID, route, turnTask, msgID, interject string, reground func()) (newTask string, changed bool) {
	// "answered" claims the reply already covers it. It does not re-anchor anything, and it does
	// NOT leave the queue here: a claim is not an answer, and dropping a user's request on an
	// assertion is the one outcome worth ruling out. Recording it stops the pending note (the
	// thing the user reads as "still queued") and hands the finish boundary a fact to check.
	if route == "answered" {
		a.markInterjectAnswered(sid, msgID, a.lastSeq(ctx, sid))
		// Say it out loud. The queue is in-memory app state; the transcript is the only thing the
		// display layer reads, so without this a bubble the model has just answered keeps its
		// "waiting" glyph and its pinned position for the rest of the turn.
		ad, _ := json.Marshal(event.InterjectionAnsweredData{MessageID: msgID})
		a.appendFact(ctx, sid, event.TypeInterjectionAnswered, event.Actor{Kind: event.ActorSystem, ID: "interject"}, ad)
		return turnTask, false
	}
	nt, changed := applyRoute(route, turnTask, interject)
	if !changed {
		return turnTask, false
	}
	a.consumeInterjectByID(ctx, sid, msgID)
	if route == "redirect" {
		reground()
	} else {
		a.injectSteerConstraint(ctx, sid, interject)
		reground()
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
func (a *App) noteInterjection(sid session.SessionID, turnTask, msgID, interject string) {
	reqLine := ""
	if h := shortReqID(msgID); h != "" {
		reqLine = "\n\nThis request's id is [req: " + h + "] — pass it as route_interjection request_id to route THIS one."
	}
	var text string
	{
		text = "magi runtime note (not user input): a new user request arrived while you are mid-task:\n" +
			clipSpec(interject, 800) + "\n\n" +
			"It has been QUEUED and will run as its own turn after you finish the current task:\n" +
			clipSpec(turnTask, 800) + "\n\n" +
			"Keep working on the current task; do not switch away from it. If — and only if — you are confident the new " +
			"request should change your direction NOW, or be folded into the current task, call route_interjection " +
			"(action \"redirect\" or \"append\") with a one-line reason. When unsure, do nothing and it stays queued.\n\n" +
			"If you simply ANSWER it in your reply — a question you can settle in a sentence, without changing what " +
			"you are doing — say so with route_interjection action \"answered\". Otherwise it stays queued and gets " +
			"handled a second time after this task, and the user keeps seeing it marked as waiting while you have " +
			"already replied."
	}
	text += reqLine
	a.mu.Lock()
	st := a.stateLocked(sid)
	if st.interjectNotes == nil {
		st.interjectNotes = map[string]string{}
	}
	st.interjectNotes[msgID] = text
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
	// An entry the agent marked "answered" is still queued (the claim is checked at the finish
	// boundary), but re-showing its note every step is the thing that reads as "I answered it and
	// it is still pending". Silence the note on the claim; the entry's fate is decided later.
	claimed := map[string]bool{}
	for _, p := range st.pendingInterject {
		if p.AnsweredAtSeq != 0 {
			claimed[p.MsgID] = true
		}
	}
	var parts []string
	if len(st.interjectNotes) > 0 {
		// Prune resolved notes; emit the live ones in a stable (msgID) order.
		ids := make([]string, 0, len(st.interjectNotes))
		for id := range st.interjectNotes {
			if deferred == nil || !deferred[id] || claimed[id] {
				delete(st.interjectNotes, id) // resolved (routed/drained/resurfaced/answered) → gone
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

// maxAsideSteps caps the triage mini-loop so it always terminates: enough to
// ask_user and then route in the same handling, but bounded against a tool-call loop.
const maxAsideSteps = 4

// asideEffect captures what an idle-park aside handling actually did to the running work, so
// handleAside can both set its queue disposition AND persist a durable audit record — the raw
// tool call/result parts stay in the mini-loop (to keep the delegated task's log clean), so
// without this the effect (a route redirect/append, a cancel) would leave no trace at all.
type asideEffect struct {
	route    string // route_interjection action that fired (redirect/append/queue), "" if none
	reason   string // route reason as given by the model
	escalate bool   // the model routed → run the steer as its own top-level turn
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
	replied, eff := a.interjectTurn(ctx, agent, s, 0, sys, aside, msgID)
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
func (a *App) interjectTurn(ctx context.Context, agent AgentSpec, s session.Session, depth int, sys, aside string, replyTo string) (replied bool, eff asideEffect) {
	// Signal/interaction tools only — the model can reply or change course but cannot start
	// (or duplicate) delegated work here.
	var specs []port.ToolSpec
	for _, name := range []string{"route_interjection", "ask_user"} {
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
			// PURE inline answer with no side effect. If the interjection ROUTED, its visible text
			// is an ack for a real action woven into the main flow, not a standalone answer, and
			// grouping it would double-move the bubble against the resurface. The flag is sticky
			// across the mini-loop's steps, so a confirmation emitted in a later tool-call-free
			// step (calls==0) still reads the earlier route and stays untagged; len(calls)>0
			// covers the same-step ack, before execAsideTool sets it.
			rt := replyTo
			if len(calls) > 0 || eff.escalate {
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
			res := a.execAsideTool(ctx, s, depth, &c, &eff)
			msgs = append(msgs, session.Message{Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: &res}}})
		}
	}
	// A route leaves no trace here: the drain's resurfaced prompt is itself the record.
	return replied, eff
}

// execAsideTool executes one signal/interaction tool call from the triage mini-turn against a
// minimal ToolEnv (only route/ask_user wired; every execution tool is nil, so the model cannot
// do delegated work here). It records whether the steer routed, which is what triageQueued
// reads to choose between answering inline and escalating to a full turn.
func (a *App) execAsideTool(ctx context.Context, s session.Session, depth int, c *session.ToolCall, eff *asideEffect) session.ToolResult {
	env := port.ToolEnv{
		SessionID: s.ID,
		RouteInterjection: func(action, reason, requestID string) error {
			// The turn has already ended, so there is no running turn to re-anchor. Any route
			// action here simply means "this needs real work" — mark it so the drain runs the
			// steer as its own fresh, fully-tooled turn.
			eff.escalate = true
			eff.route = action
			if reason != "" {
				eff.reason = reason
			}
			return nil
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

// ---- interjection / steer state accessors (moved from app.go) ----
// State-management layer for the mid-turn steer machinery above: turnControl signals,
// the pending-interjection queue, and the interjection-seen mask. All guard a.mu.
