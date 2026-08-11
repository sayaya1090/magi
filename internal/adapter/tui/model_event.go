package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// answerQuestion resolves the open ask_user modal with the picked option ("" =
// dismissed) and hands it to the blocked tool execution.
func (m *Model) answerQuestion(answer string) tea.Cmd {
	q := m.quest
	m.quest = nil
	sid := m.sid
	m.refresh()
	return func() tea.Msg {
		_ = m.app.RespondQuestion(m.ctx, command.RespondQuestion{
			SessionID: sid, CallID: q.callID, Answer: answer,
			Actor: event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	}
}

func (m *Model) respond(decision string) tea.Cmd {
	p := m.perm
	m.perm = nil
	sid := p.sid // the session that raised it (a subagent child, not always the main turn)
	if sid == "" {
		sid = m.sid
	}
	return func() tea.Msg {
		_ = m.app.RespondPermission(m.ctx, command.RespondPermission{
			SessionID: sid, CallID: p.callID, Decision: decision,
			Actor: event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	}
}

// reviveForEngineActivity turns the running indicator back on when the engine produces
// generation/tool activity while the TUI believes the turn is idle. m.running and the
// spinner are armed CLIENT-side on submit (model_submit), not by an engine turn-start
// event — so a turn that re-runs entirely engine-side is invisible: a prompt that landed
// mid-COUNCIL is re-processed after the previous turn already emitted TurnFinished
// (m.running=false, turnReqID cleared), yet no client submit fires to re-arm the spinner.
// Without this the new turn runs silently — the last answer looks complete and idle while
// work is actually happening. Any real generation (a PartDelta token, a tool's live
// progress) proves a turn is running, so re-arm and claim the spinner for the most recent
// user bubble (the prompt now being answered).
func (m *Model) reviveForEngineActivity() {
	if m.running {
		return
	}
	m.running = true
	// Start the clock too. The meter reads time.Since(turnStart), and a turn revived from engine
	// activity had never been through the submit path that sets it — so a zero start rendered
	// "working… 2562047h47m", which is what time.Since(zero) is. Found by a random session.
	if m.turnStart.IsZero() {
		m.turnStart = time.Now()
	}
	if m.turnReqID == "" {
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockUser && m.blocks[i].reqID != "" {
				m.turnReqID = m.blocks[i].reqID
				break
			}
		}
	}
}

// applyEvent folds a domain event into the transcript state.
func (m *Model) applyEvent(e event.Event) {
	// Engine-side generation/tool activity means a turn is running even if the TUI armed
	// no spinner for it (an engine-initiated re-run, e.g. a mid-council prompt). Re-arm.
	switch e.Type {
	case event.TypePartDelta, event.TypeToolProgress:
		m.reviveForEngineActivity()
	}
	switch e.Type {
	case event.TypePromptSubmitted:
		// A subagent result injected into the parent (actor=agent) is swallowed here:
		// the full text lives in that subagent's own pane/detail view, so surfacing a
		// per-report one-liner in the main transcript is just noise. Real user prompts
		// are added locally (submit/steer), so they're not handled here.
		if e.Actor.Kind == event.ActorAgent {
			return
		}
		// The user prompt was added locally (submit/steer) before the engine minted its
		// MessageID; this event carries it. Bind it to the local bubble (reqID) so the
		// bubble can show the in-flight spinner and later pair with an inline answer.
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			return
		}
		text := joinTextParts(d.Parts)
		if e.Actor.Kind == event.ActorSystem {
			// System-injected context (planner/orient/specmine/council notes): the model
			// sees these as guidance, but the TUI silently dropped them — headless prints
			// a "⟳ <id> note:" line, so the richer surface showed LESS than the plain one
			// (a user could not see the mined note at all). Render the first line as an
			// info row; the full text stays in the transcript store.
			head := text
			if i := strings.IndexByte(head, '\n'); i > 0 {
				head = head[:i]
			}
			m.blocks = append(m.blocks, block{kind: blockInfo, text: "⟳ " + e.Actor.ID + " note: " + clipLine(head, 160)})
			return
		}
		if d.ResurfacedFrom != "" {
			// A queued interjection re-surfacing as its own turn: while queued the original
			// bubble stayed at its input position; now its answer is about to render, so pull
			// that bubble (keyed by its reqID == ResurfacedFrom) down to just above the
			// answer so the query/answer form a pair. This resurfaced prompt IS the new turn.
			m.blocks = moveUserBlockToEnd(m.blocks, d.ResurfacedFrom, text)
			m.cache = m.cache[:0] // reorder shifts block indices — the prefix cache is now stale
			m.turnReqID = d.ResurfacedFrom
			return
		}
		// The bubble is normally already there: the submit path adds it locally the moment the
		// user presses enter, and this event only binds the request id to it. On a RESUMED
		// session there was no such moment — the log is the only source — and the return value
		// saying "nothing to stamp" was discarded, so a reopened session rendered magi's answers
		// with none of the questions. If no bubble carries this text, the record says the user
		// said it, so put it on screen.
		if !stampUserReqID(m.blocks, text, d.MessageID) && strings.TrimSpace(text) != "" {
			m.blocks = append(m.blocks, block{kind: blockUser, reqID: d.MessageID, text: text, ts: e.TS})
		}
		if m.awaitingTurnReqID {
			// This is the fresh submit that started the running turn — it owns the spinner.
			// A queued interjection landing mid-turn arrives with the flag already cleared,
			// so it never steals the spinner from the request being processed.
			m.turnReqID = d.MessageID
			m.awaitingTurnReqID = false
		}

	case event.TypePartDelta:
		var d event.PartDeltaData
		if json.Unmarshal(e.Data, &d) == nil {
			switch d.Kind {
			case session.PartText:
				m.liveText += d.Text
			case session.PartReasoning:
				m.liveThink += d.Text
			}
		}

	case event.TypeToolProgress:
		// A long-running tool's live poll status (wait_for). Keep only the latest
		// note; it renders as one ephemeral line by the spinner and is dropped when
		// the tool's result lands (see PartToolResult below) or the turn ends.
		var d event.ToolProgressData
		if json.Unmarshal(e.Data, &d) == nil {
			m.liveProgress = strings.TrimSpace(d.Text)
		}

	case event.TypePartAppended:
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			return
		}
		m.onPartAppended(d, e.TS)

	case event.TypePermissionRequested:
		var d event.PermissionRequestedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.perm = &permReq{sid: m.sid, callID: d.CallID, name: d.Name, args: string(d.Args), reason: d.Reason}
		}

	case event.TypeQuestionRequested:
		var d event.QuestionRequestedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.quest = &questReq{callID: d.CallID, question: d.Question, options: d.Options,
				report: d.Report}
		}

	case event.TypeContextUsage:
		var d event.ContextUsageData
		if json.Unmarshal(e.Data, &d) == nil {
			m.ctxPct = d.Percent
			m.ctxTokens = d.Tokens  // used, for the persistent footer gauge (survives turn reset)
			m.ctxWindow = d.Window  // max window (0 = unknown), for the footer gauge
			m.turnIn = d.Tokens     // ↑ current context (§8.1)
			m.turnOut = d.OutTokens // ↓ cumulative output so far
		}

	case event.TypeTodosChanged:
		// The plan the agent keeps, which the panel is mostly made of.
		//
		// In one process this changes nothing: the engine writing the plan and the engine this
		// asks are the same object. Attached to a daemon they are not, and nothing else closed the
		// gap — the plan is read from session state held in memory, filled once when a session is
		// opened and never again, so a plan made after attaching was invisible for as long as the
		// viewer stayed attached. The panel is hidden when there is no plan, so what a person saw
		// was a companion working through a plan it appeared not to have.
		//
		// Written INTO the engine rather than kept beside it, because everything that draws the
		// plan asks the engine for it — the panel, the transcript's step marks, /plan. A copy here
		// would be a second answer that the three of them would drift between.
		var d event.TodosChangedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.app.SetTodos(m.sid, d.Todos)
		}

	case event.TypeModelChanged:
		// The session's active model changed at runtime (plugin set_model, /route
		// edit, reload_config) — refresh the cached header chip and, if the routing
		// editor is open, its rows, from this one signal.
		var d event.ModelChangedData
		if json.Unmarshal(e.Data, &d) == nil && d.Model != "" {
			m.model = d.Model
			if m.routing {
				m.refreshRouteList()
			}
		}

	case event.TypeUserLabelChanged:
		// A plugin set the user's display label (e.g. an SSO plugin injecting the
		// authenticated username). Update it and drop the block cache so already-
		// rendered user blocks re-render with the new label, not the stale "you".
		var d event.UserLabelData
		if json.Unmarshal(e.Data, &d) == nil && d.Label != "" {
			m.userLbl = d.Label
			m.cache = m.cache[:0]
			m.dirty = true
		}

	case event.TypeWorkflowPhase:
		var d event.WorkflowPhaseData
		if json.Unmarshal(e.Data, &d) == nil && d.Phase == "plan" {
			m.plannerMode = d.Status // header chip (compact: solo | parallel)
			// Also surface the planner's decision + reason as a visible line.
			info := "◈ planner: " + d.Status
			if d.Detail != "" {
				info += " — " + d.Detail
			}
			m.blocks = append(m.blocks, block{kind: blockInfo, text: info})
		}

	case event.TypeCompaction:
		// Context compaction shed older history into a summary. In the TUI this
		// otherwise happens invisibly, so surface it as a transcript line stating the
		// before→after size AND the difference (freed tokens + percent), matching the
		// headless printer — the user sees how much room was reclaimed, not just that
		// it happened.
		var d event.CompactionData
		if json.Unmarshal(e.Data, &d) == nil {
			m.blocks = append(m.blocks, block{kind: blockInfo, text: fmt.Sprintf(
				"↯ context compacted: ~%d→%d tok (%s)", d.TokensBefore, d.TokensAfter, d.SizeNote())})
		}

	case event.TypeInterjectionAnswered:
		// The agent says its reply already covered this queued message. Two things were wrong
		// without it, both visible to the user: the bubble kept the "waiting" glyph forever, and
		// the queued-tail hoist pinned it below every later block so it never scrolled away. Pair
		// it with the answer the way an inline reply is paired — the question sits just above the
		// text that answers it, and from then on it moves with the transcript like anything else.
		var d event.InterjectionAnsweredData
		if json.Unmarshal(e.Data, &d) == nil && d.MessageID != "" {
			// The step's assistant text is persisted BEFORE its tool calls run, so the answer is
			// already the last assistant block by the time this claim arrives. moveUserBlockBefore
			// takes the ANSWER's index — passing the slice length puts the question after it, which
			// is the arrangement this whole signal exists to undo.
			for i := len(m.blocks) - 1; i >= 0; i-- {
				if m.blocks[i].kind != blockAssistant {
					continue
				}
				var moved bool
				if m.blocks, moved = moveUserBlockBefore(m.blocks, d.MessageID, i); moved {
					m.cache = m.cache[:0] // reorder shifts block indices — the prefix cache is stale
				}
				break
			}
			// moveUserBlockBefore clears `queued` only when it actually moved the block. A bubble
			// already sitting in the right place must lose the glyph too — being in position is not
			// the same as still waiting.
			//
			// Clearing the flag is only half of it: a finalized block is served from the render
			// cache, so the bubble kept drawing the waiting dot with `queued` already false. The
			// reorder path above drops the whole cache and hid this; the in-place path did not,
			// and that is the arrangement the signal is MOST likely to arrive in — the message was
			// typed last, so it is already at the tail with nothing to move it past.
			for i := range m.blocks {
				if m.blocks[i].kind == blockUser && m.blocks[i].reqID == d.MessageID && m.blocks[i].queued {
					m.blocks[i].queued = false
					if i < len(m.cache) {
						m.cache = m.cache[:i] // re-render it without the waiting glyph
					}
				}
			}
		}

	case event.TypeCouncilConvened:
		// A council round opened: record it as a transcript milestone and arm the
		// header chip. (D14 — the consensus termination gate.)
		var d event.CouncilConvenedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.onCouncilConvened(d)
		}

	case event.TypeCouncilDeliberating:
		// Live: which member is being polled right now (header chip).
		var d event.CouncilDeliberatingData
		if json.Unmarshal(e.Data, &d) == nil {
			m.councilRound = d.Round
			m.councilMember = d.Member
		}

	case event.TypeCouncilVerdict:
		// ONE line per member, printed when that member answers and never redrawn.
		//
		// The transcript is inline — tea.NewProgram with no alt screen — so bubbletea repaints
		// only the lines it still owns and anything scrolled above is permanent. A row that grew
		// as each member landed therefore left every earlier version of itself on screen: one
		// member, then two, then three. Reported exactly that way once the verdicts began
		// streaming, which turned a redraw that used to finish inside one frame into three
		// separated by a minute.
		//
		// So nothing is ever redrawn. Each member gets its own line as it lands, and the reader
		// watches the round fill in a line at a time.
		var d event.CouncilVerdictData
		if json.Unmarshal(e.Data, &d) == nil {
			// A member arrives twice — the live preview when it answers, then the recorded fact
			// when the round closes. Identical ones are the same news and print once. A fact that
			// DIFFERS is a rebuttal round having changed that member's mind, which is news of its
			// own and earns its own line rather than silently replacing what was read already.
			if prev, seen := m.drawnVerdicts[d.Member]; seen && prev == d {
				break
			}
			if m.drawnVerdicts == nil {
				m.drawnVerdicts = map[string]event.CouncilVerdictData{}
			}
			m.drawnVerdicts[d.Member] = d
			m.blocks = append(m.blocks, block{kind: blockCouncilVerdict,
				councilVerdicts: []event.CouncilVerdictData{d}, evidence: m.pendingCouncilEvidence})
		}

	case event.TypeCouncilDecided:
		// The round's outcome + tally. A "continue" injects feedback and the loop
		// runs again; "done" (or a noted forced finish) lets the turn end. Clear the
		// chip: between rounds the agent is working, not deliberating, so the council
		// indicator should show only during an open round (convened→decided).
		if m.councilRound > m.turnCouncil {
			m.turnCouncil = m.councilRound // remember the deepest round for the turn summary
		}
		m.councilRound = 0
		m.councilMember = ""
		m.councilPhase = ""
		var d event.CouncilDecidedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.onCouncilDecided(d)
		}

	case event.TypePromptAbandoned:
		// This request will never be answered: its turn was cancelled, or it was coalesced into a
		// later one that carries its text. Nothing said so, so the bubble kept its queued glyph —
		// on screen now and again on every resume — and a dropped request looked exactly like one
		// still waiting its turn. Mark the bubble instead of adding a line: the fact is about that
		// message, and a separate notice would be read as being about the newest one.
		var d event.PromptAbandonedData
		if json.Unmarshal(e.Data, &d) == nil && d.MsgID != "" {
			for i := range m.blocks {
				if m.blocks[i].kind == blockUser && m.blocks[i].reqID == d.MsgID {
					m.blocks[i].queued, m.blocks[i].abandoned = false, true
					m.cache = m.cache[:min(i, len(m.cache))] // re-render it (the cache is append-only)
					break
				}
			}
		}

	case event.TypeTurnFinished:
		m.onTurnFinished(e)

	case event.TypeError:
		var d event.ErrorData
		_ = json.Unmarshal(e.Data, &d)
		m.running = false
		m.clearSpinnerCache(m.turnReqID)
		m.turnReqID = ""
		m.awaitingTurnReqID = false
		m.liveText = ""
		m.liveThink = ""
		m.liveProgress = ""
		m.activeAgents = nil
		m.councilRound = 0
		m.councilMember = ""
		m.councilPhase = ""
		m.reviewFoldNext = false   // an errored turn's revision never lands — keep the report
		if !m.turnStart.IsZero() { // freeze the meter too (mirror panes) (§8.1)
			m.turnDur = time.Since(m.turnStart)
		}
		// An error ends the turn as surely as a finish does, and a bubble still flagged queued is
		// waiting for a turn that is over — nothing is coming to pick it up. onTurnFinished has
		// always cleared these; this path never did, so an errored turn left a request marked
		// "waiting" for the rest of the session, and (since the queued run renders last) pinned it
		// to the bottom of the transcript where it could not scroll away.
		m.clearQueuedGlyphs()
		m.blocks = append(m.blocks, block{kind: blockError, text: d.Message})
	}
}

// onPartAppended folds a finalized transcript part (reasoning/text/tool call or
// result) into the block list, deduping a near-identical re-sent answer and
// tracking per-turn step/file counts.
func (m *Model) onPartAppended(d event.PartAppendedData, ts time.Time) {
	switch d.Part.Kind {
	case session.PartReasoning:
		m.liveThink = ""
		m.blocks = append(m.blocks, block{kind: blockReasoning, text: d.Part.Text})
	case session.PartText:
		m.liveText = ""
		// The revision promised by a review-round "continue" has now landed. THIS is the
		// first moment we can tell whether it actually changed: the pre-review report is
		// still in m.blocks and the revision is d.Part.Text (at council.decided the
		// revision didn't exist yet, which is why the fold is deferred to here).
		//   • Unchanged→ the model re-sent the same answer. Keep the original exactly where
		//     it is and DROP the duplicate silently — don't fold it and don't re-print it,
		//     so the report never blinks out and reappears identical (which just reads as a
		//     glitch).
		//   • Changed  → fold the superseded original to a stub; the revision renders full.
		// The fold runs before the generic sameAnswer dedup, so once folded lastAssistantText
		// finds no prior assistant block and the changed revision always appends in full.
		if m.reviewFoldNext {
			m.reviewFoldNext = false
			if prev := m.lastAssistantText(); prev != "" && sameAnswer(prev, d.Part.Text) {
				break // identical revision: leave the original untouched, drop the re-send
			}
			m.collapseReviewedReport()
		}
		// A long answer the council rejected and the model re-sent nearly
		// verbatim is brutal to re-read. Collapse an assistant block that
		// duplicates the previous assistant block in this turn to a stub.
		if prev := m.lastAssistantText(); prev != "" && len(d.Part.Text) > 400 && sameAnswer(prev, d.Part.Text) {
			m.blocks = append(m.blocks, block{kind: blockInfo, text: "≡ (이전 답변과 사실상 동일한 재응답 — 생략)"})
			break
		}
		m.blocks = append(m.blocks, block{kind: blockAssistant, text: d.Part.Text, ts: ts})
		// An inline interjection answer (InReplyTo set) never resurfaces as its own turn,
		// so its question bubble is stranded up at its input position. Pull it down to just
		// above this answer so the two read as a [question → answer] group lifted out of
		// the main-task flow, question on top. No-op if the question can't be located.
		if d.InReplyTo != "" {
			var moved bool
			m.blocks, moved = moveUserBlockBefore(m.blocks, d.InReplyTo, len(m.blocks)-1)
			if moved {
				m.cache = m.cache[:0] // reorder shifts block indices — the prefix cache is stale
			}
		}
	case session.PartToolCall:
		if d.Part.ToolCall != nil {
			m.liveText = ""
			m.turnSteps++
			switch d.Part.ToolCall.Name {
			case "write", "edit", "multiedit":
				var fa struct {
					Path string `json:"path"`
				}
				if json.Unmarshal(d.Part.ToolCall.Args, &fa) == nil && fa.Path != "" {
					if m.turnFiles == nil {
						m.turnFiles = map[string]bool{}
					}
					m.turnFiles[fa.Path] = true
				}
			}
			m.blocks = append(m.blocks, block{
				kind:   blockToolCall,
				name:   d.Part.ToolCall.Name,
				args:   string(d.Part.ToolCall.Args),
				callID: d.Part.ToolCall.CallID,
			})
		}
	case session.PartToolResult:
		m.liveProgress = "" // the tool that was reporting progress has now returned
		if d.Part.ToolResult != nil {
			m.foldToolResult(d.Part.ToolResult.CallID, toolResultText(d.Part.ToolResult), !d.Part.ToolResult.IsError)
		}
	}
}

// onCouncilConvened records a newly-opened council round as a transcript
// milestone (when it carries round-specific signals or a plan procedure) and
// arms the header chip. (D14 — the consensus termination gate.)
func (m *Model) onCouncilConvened(d event.CouncilConvenedData) {
	m.councilRound = d.Round
	m.pendingCouncilEvidence = formatCouncilEvidence(d) // shown in each verdict's detail view
	// No transcript line. The convened line repeated members + rule every round, which the
	// header chip already shows live and each verdict repeats as it lands, so it only earned
	// one when it carried something round-specific — and the only thing that ever did was the
	// signals list, which no longer exists.
}

// onCouncilDecided renders a round's outcome + tally line. The caller clears the
// live council chip before invoking this (a decision ends the open round).
func (m *Model) onCouncilDecided(d event.CouncilDecidedData) {
	m.drawnVerdicts = nil // the round is closed; the next one starts with nothing printed
	_, verdict := councilVerdictLabel(d.Decision)
	counts := fmt.Sprintf("%d done / %d continue", d.Tally.Done, d.Tally.Continue)
	if d.Tally.Abstain > 0 {
		counts += fmt.Sprintf(" / %d abstain", d.Tally.Abstain)
	}
	line := fmt.Sprintf("⚖ council round %d: %s — %s", d.Round, verdict, counts)
	// A rebuttal only runs when the independent vote split, and the tally beside it is the
	// one taken AFTER it — so without saying so, a 3-0 that started 2-1 reads as agreement
	// that was never there. Name the move, and name a rebuttal that moved nobody too: that
	// the members heard each other and did not budge is the more interesting outcome.
	if db := d.Debate; db != nil {
		moved := fmt.Sprintf("%d members", db.Changed)
		if db.Changed == 1 {
			moved = "1 member"
		}
		switch {
		case db.Changed == 0:
			line += " [debated, no one moved]"
		case db.Before != db.After:
			line += fmt.Sprintf(" [debated: %s→%s, %s moved]", db.Before, db.After, moved)
		default:
			line += fmt.Sprintf(" [debated: %s held, %s moved]", db.After, moved)
		}
	}
	if d.Note != "" {
		line += " (" + d.Note + ")"
	} else if d.Feedback != "" {
		line += " → feedback injected"
	}
	// A round that reused a standing rejection WITHOUT deliberating (no new tool actions since the
	// last one) emits no verdicts, so nothing on screen says what it is still holding the turn open
	// over — only "→ feedback injected", while the injected text itself lands in the transcript as a
	// system note clipped to its first line. Print the demand under the decision in exactly that
	// case: when this round did deliberate, each member's own reason already renders under its
	// verdict row and repeating the aggregate would just say it twice.
	if len(m.roundVerdicts(d.Round)) == 0 {
		for _, ln := range d.FeedbackLines() {
			line += "\n    " + ln
		}
	}
	m.blocks = append(m.blocks, block{kind: blockInfo, text: line})
	// A review round (non-plan) that votes "continue" re-prompts the model for a
	// revised answer (council_gate emitDecided(Continue,…) → re-run), so the report
	// just shown is about to be superseded. DON'T fold it yet: the revision might never
	// materialize (interrupt, error, or a tool-only forced finish), and folding in place
	// would leave a "아래 최종본 참고" stub pointing at nothing. Arm a deferred fold
	// instead — collapseReviewedReport runs from onPartAppended the moment the actual
	// replacement lands. Forced finishes come through as "done", so they never arm this.
	if d.Decision == "continue" {
		m.reviewFoldNext = true
	}
}

// clearSpinnerCache drops the prefix-cache entry (and the tail after it) for the bubble
// that was spinning under reqID, so it re-renders as ▌ now that the turn is over. A mid-turn
// full cache reset (resize/theme/reasoning toggle) can re-cache the in-flight bubble WITH a
// spinner frame; without this, that frozen glyph would persist past turn end until the next
// reset. No-op when reqID is empty or the bubble was never cached.
func (m *Model) clearSpinnerCache(reqID string) {
	if reqID == "" {
		return
	}
	for i := range m.blocks {
		if m.blocks[i].kind == blockUser && m.blocks[i].reqID == reqID {
			if i < len(m.cache) {
				m.cache = m.cache[:i]
			}
			return
		}
	}
}

// onTurnFinished tears down live-turn state (running flag, streaming buffers,
// council chip, active panes), freezes the turn meter from final usage, and
// appends the one-line turn receipt.
func (m *Model) onTurnFinished(e event.Event) {
	m.running = false
	m.clearSpinnerCache(m.turnReqID)
	m.turnReqID = "" // the request's bubble reverts from spinner to ▌
	m.awaitingTurnReqID = false
	// The turn ended and the interjection queue drained: any bubble still flagged queued
	// (waiting mid-turn) is no longer waiting — clear the glyph. Inline-answered/resurfaced
	// bubbles already cleared theirs; this catches any that weren't reached.
	m.clearQueuedGlyphs()
	m.liveText = ""
	m.liveThink = ""
	m.liveProgress = ""
	m.activeAgents = nil
	m.councilRound = 0
	m.councilMember = ""
	m.councilPhase = ""
	m.drawnVerdicts = nil
	m.reviewFoldNext = false // turn ended with no revision landing — keep the report visible
	// The turn ended: any pane still marked unfinished (e.g. a completion event
	// that never arrived) should now fade too, so nothing lingers after the turn.
	for _, p := range m.panes {
		if !p.done {
			p.done = true
			p.doneAt = time.Now()
			if p.dur == 0 && !p.started.IsZero() {
				p.dur = time.Since(p.started) // freeze the meter too, else it keeps climbing
			}
		}
	}
	// Freeze the turn meter from the cumulative usage (§8.1).
	if !m.turnStart.IsZero() {
		m.turnDur = time.Since(m.turnStart)
	}
	var fd event.TurnFinishedData
	if json.Unmarshal(e.Data, &fd) == nil {
		if fd.Usage.In > 0 {
			m.turnIn = fd.Usage.In
		}
		if fd.Usage.Out > 0 {
			m.turnOut = fd.Usage.Out
		}
		m.turnUnverified = fd.Unverified
	}
	// One-line turn receipt: what the turn actually cost, without scrolling
	// back through the transcript (steps matter more now that the ceiling is 240).
	//
	// ONCE per turn. A turn can end more than once on this stream — a cancelled run writes a
	// second turn.finished, and the run goroutine publishes a transient one when it retires so a
	// spinner revived by work that arrived after the turn's own event still stops. Both carry the
	// same counters, so an unguarded append printed the identical receipt twice. The flag is
	// cleared by SUBMIT, not by the spinner reviving: the receipt belongs to the turn the user
	// asked for, and a drained interjection answered afterwards is part of it, not a new one.
	if m.turnReceipted {
		return
	}
	if sum := m.turnSummary(); sum != "" {
		m.blocks = append(m.blocks, block{kind: blockInfo, text: sum})
		m.turnReceipted = true
	}
}

// clearQueuedGlyphs drops the waiting marker from every bubble that still carries one, and
// invalidates the cache from the first of them. Shared by the two ways a turn can end: whichever
// one happens, nothing is left to pick a queued request up, so continuing to show it as waiting
// tells the user to expect something that is not coming.
func (m *Model) clearQueuedGlyphs() {
	for i := range m.blocks {
		if m.blocks[i].queued {
			m.blocks[i].queued = false
			if i < len(m.cache) {
				m.cache = m.cache[:i]
			}
		}
	}
}
