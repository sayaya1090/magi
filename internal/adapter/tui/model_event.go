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
	sid := m.sid
	return func() tea.Msg {
		_ = m.app.RespondPermission(m.ctx, command.RespondPermission{
			SessionID: sid, CallID: p.callID, Decision: decision,
			Actor: event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	}
}

// applyEvent folds a domain event into the transcript state.
func (m *Model) applyEvent(e event.Event) {
	switch e.Type {
	case event.TypePromptSubmitted:
		// A subagent result injected into the parent (actor=agent) is swallowed here:
		// the full text lives in that subagent's own pane/detail view, so surfacing a
		// per-report one-liner in the main transcript is just noise. Real user prompts
		// are added locally (submit/steer), so they're not handled here.
		if e.Actor.Kind == event.ActorAgent {
			return
		}
		// A queued interjection re-surfacing as its own turn (ResurfacedFrom set): while
		// queued the original bubble stayed at its input position; now its answer is
		// about to render, so pull that bubble down to just above the answer — remove the
		// stranded original and re-append it at the end so the query/answer form a pair.
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil && d.ResurfacedFrom != "" {
			text := joinTextParts(d.Parts)
			m.blocks = moveUserBlockToEnd(m.blocks, text)
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

	case event.TypePartAppended:
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			return
		}
		m.onPartAppended(d)

	case event.TypeAgentSpawned:
		var d event.AgentStatusData
		if json.Unmarshal(e.Data, &d) == nil {
			m.activeAgents = append(m.activeAgents, orDefaultStr(d.Role, d.AgentID))
		}

	case event.TypeAgentStatus:
		var d event.AgentStatusData
		if json.Unmarshal(e.Data, &d) == nil && d.State == "done" {
			m.activeAgents = removeFirst(m.activeAgents, orDefaultStr(d.Role, d.AgentID))
			p := m.paneBySID(session.SessionID(d.AgentID))
			fadeDbg("main: AgentStatus done agentID=%s paneFound=%v", shortSID(session.SessionID(d.AgentID)), p != nil)
			if p != nil && !p.done {
				p.done = true
				p.doneAt = time.Now() // start this pane's own fade clock
			}
		}

	case event.TypePermissionRequested:
		var d event.PermissionRequestedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.perm = &permReq{callID: d.CallID, name: d.Name, args: string(d.Args), reason: d.Reason}
		}

	case event.TypeQuestionRequested:
		var d event.QuestionRequestedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.quest = &questReq{callID: d.CallID, question: d.Question, options: d.Options}
		}

	case event.TypeContextUsage:
		var d event.ContextUsageData
		if json.Unmarshal(e.Data, &d) == nil {
			m.ctxPct = d.Percent
			m.turnIn = d.Tokens     // ↑ current context (§8.1)
			m.turnOut = d.OutTokens // ↓ cumulative output so far
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
		// A round's votes share ONE compact line (member-colored icon + name + decision);
		// each member's full reasoning is kept on the block and shown in a detail modal
		// when that member is clicked. Append to the current round's row, or start one.
		var d event.CouncilVerdictData
		if json.Unmarshal(e.Data, &d) == nil {
			if n := len(m.blocks); n > 0 && m.blocks[n-1].kind == blockCouncilVerdict &&
				len(m.blocks[n-1].councilVerdicts) > 0 && m.blocks[n-1].councilVerdicts[0].Round == d.Round {
				m.blocks[n-1].councilVerdicts = append(m.blocks[n-1].councilVerdicts, d)
				// The render cache is append-only: a block already cached is never
				// re-rendered. Members vote concurrently and their verdicts stream in
				// back-to-back, so if a frame is painted after the first verdict but
				// before the rest, the block gets cached as a single-member line and the
				// later members never appear. Drop this block's cache entry so it
				// re-renders with the full row.
				if len(m.cache) > n-1 {
					m.cache = m.cache[:n-1]
				}
			} else {
				m.blocks = append(m.blocks, block{kind: blockCouncilVerdict, councilVerdicts: []event.CouncilVerdictData{d}, evidence: m.pendingCouncilEvidence})
			}
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
		var d event.CouncilDecidedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.onCouncilDecided(d)
		}

	case event.TypeTurnFinished:
		m.onTurnFinished(e)

	case event.TypeError:
		var d event.ErrorData
		_ = json.Unmarshal(e.Data, &d)
		m.running = false
		m.liveText = ""
		m.liveThink = ""
		m.activeAgents = nil
		m.councilRound = 0
		m.councilMember = ""
		if !m.turnStart.IsZero() { // freeze the meter too (mirror panes) (§8.1)
			m.turnDur = time.Since(m.turnStart)
		}
		m.blocks = append(m.blocks, block{kind: blockError, text: d.Message})
	}
}

// onPartAppended folds a finalized transcript part (reasoning/text/tool call or
// result) into the block list, deduping a near-identical re-sent answer and
// tracking per-turn step/file counts.
func (m *Model) onPartAppended(d event.PartAppendedData) {
	switch d.Part.Kind {
	case session.PartReasoning:
		m.liveThink = ""
		m.blocks = append(m.blocks, block{kind: blockReasoning, text: d.Part.Text})
	case session.PartText:
		m.liveText = ""
		// A long answer the council rejected and the model re-sent nearly
		// verbatim is brutal to re-read. Collapse an assistant block that
		// duplicates the previous assistant block in this turn to a stub.
		if prev := m.lastAssistantText(); prev != "" && len(d.Part.Text) > 400 && sameAnswer(prev, d.Part.Text) {
			m.blocks = append(m.blocks, block{kind: blockInfo, text: "≡ (이전 답변과 사실상 동일한 재응답 — 생략)"})
			break
		}
		m.blocks = append(m.blocks, block{kind: blockAssistant, text: d.Part.Text})
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
				kind: blockToolCall,
				name: d.Part.ToolCall.Name,
				args: string(d.Part.ToolCall.Args),
			})
		}
	case session.PartToolResult:
		if d.Part.ToolResult != nil {
			m.foldToolResult(toolResultText(d.Part.ToolResult), !d.Part.ToolResult.IsError)
		}
	}
}

// onCouncilConvened records a newly-opened council round as a transcript
// milestone (when it carries round-specific signals or a plan procedure) and
// arms the header chip. (D14 — the consensus termination gate.)
func (m *Model) onCouncilConvened(d event.CouncilConvenedData) {
	m.councilRound = d.Round
	m.pendingCouncilEvidence = formatCouncilEvidence(d) // shown in each verdict's detail view
	label, verb := "council", "deliberate"
	if d.Phase == "plan" {
		label, verb = "plan audit", "review the plan"
	}
	// Noise control: the routine convened line (members + rule) repeats the
	// same information every round and the header chip already shows the
	// live deliberation — so it is only worth a transcript line when it
	// carries something round-specific: deterministic SIGNALS (fabrication
	// self-check, verify commands) or a plan-audit's procedure.
	showLine := len(d.Signals) > 0 || d.Phase == "plan"
	line := fmt.Sprintf("⚖ %s round %d — %s %s (%s)", label, d.Round, strings.Join(d.Members, ", "), verb, d.Rule)
	if len(d.Signals) > 0 {
		line += " · " + strings.Join(d.Signals, ", ")
	}
	// Plan audit: show the procedure being judged THIS round, so a revised plan
	// that gets rejected and replanned stays visible (you can see what changed
	// across rounds, not just the final one that ran).
	if d.Phase == "plan" {
		if plan := strings.TrimSpace(d.Plan); plan != "" {
			for _, pl := range strings.Split(plan, "\n") {
				line += "\n    " + pl
			}
		}
	}
	if showLine {
		m.blocks = append(m.blocks, block{kind: blockInfo, text: line})
	}
}

// onCouncilDecided renders a round's outcome + tally line. The caller clears the
// live council chip before invoking this (a decision ends the open round).
func (m *Model) onCouncilDecided(d event.CouncilDecidedData) {
	label := "council"
	if d.Phase == "plan" {
		label = "plan audit"
	}
	// A plan re-plan (continue) is always critical-driven (the severity veto), so the
	// round word is "revise"; termination continue → "reject".
	_, verdict := councilVerdictLabel(d.Phase, d.Decision, "critical")
	if strings.Contains(d.Note, "finishing") || strings.Contains(d.Note, "proceeding") {
		// Any forced finish (round cap OR no-progress) — not a real approval/done.
		// Normal consensus decisions carry no note; error fallbacks read as-is.
		verdict = "finished (no consensus)"
		if d.Phase == "plan" {
			verdict = "proceed (no consensus)"
		}
	}
	// Tally: for a plan audit, split the continue votes by severity tier (revise /
	// advise / note) so the count matches the per-member labels and shows what was
	// blocking vs advisory — not a flat "N revise".
	counts := ""
	if d.Phase == "plan" {
		counts = planTierTally(m.roundVerdicts(d.Round))
	}
	if counts == "" {
		doneLabel, contLabel := "done", "continue"
		if d.Phase == "plan" {
			doneLabel, contLabel = "approve", "revise"
		}
		counts = fmt.Sprintf("%d %s / %d %s", d.Tally.Done, doneLabel, d.Tally.Continue, contLabel)
		if d.Tally.Abstain > 0 {
			counts += fmt.Sprintf(" / %d abstain", d.Tally.Abstain)
		}
	}
	line := fmt.Sprintf("⚖ %s round %d: %s — %s", label, d.Round, verdict, counts)
	if d.Note != "" {
		line += " (" + d.Note + ")"
	} else if d.Feedback != "" {
		line += " → feedback injected"
	}
	m.blocks = append(m.blocks, block{kind: blockInfo, text: line})
}

// onTurnFinished tears down live-turn state (running flag, streaming buffers,
// council chip, active panes), freezes the turn meter from final usage, and
// appends the one-line turn receipt.
func (m *Model) onTurnFinished(e event.Event) {
	m.running = false
	m.liveText = ""
	m.liveThink = ""
	m.activeAgents = nil
	m.councilRound = 0
	m.councilMember = ""
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
	if sum := m.turnSummary(); sum != "" {
		m.blocks = append(m.blocks, block{kind: blockInfo, text: sum})
	}
}
