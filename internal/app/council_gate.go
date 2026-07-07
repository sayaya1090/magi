package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// councilDiffCap / councilSignalCap bound the diff and verify output embedded in
// each member's prompt so they don't multiply token cost by the member count.
const (
	councilDiffCap    = 6000
	councilSignalCap  = 2000
	councilActionsCap = 8   // most recent turn outputs (model text + tool results) shown to the council
	councilActionCap  = 400 // per-item byte cap
)

// turnToolEvidence summarizes THIS turn's tool RESULTS as real, git-independent
// evidence of what actually happened — a write that reported bytes, a `cat` that shows
// the content. It deliberately EXCLUDES the model's own text: that is the agent's claim
// (already passed as Report), and admitting narration as "evidence" is exactly how a
// defeatist agent talks the council into "done" with no artifact (the download-youtube
// lesson). Only events since the last user prompt are considered, so a prior turn's
// successful tool result can't masquerade as this turn's. Most recent k results.
func turnToolEvidence(evs []event.Event, k int) string {
	names := map[string]string{} // callID -> tool name
	var lines []string
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted { // new turn boundary → keep only the latest turn's evidence
			names = map[string]string{}
			lines = nil
			continue
		}
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		switch d.Part.Kind {
		case session.PartToolCall:
			if d.Part.ToolCall != nil {
				names[d.Part.ToolCall.CallID] = d.Part.ToolCall.Name
			}
		case session.PartToolResult:
			if d.Part.ToolResult == nil {
				continue
			}
			name := names[d.Part.ToolResult.CallID]
			if name == "" {
				name = "tool"
			}
			status := "ok"
			if d.Part.ToolResult.IsError {
				status = "error"
			}
			lines = append(lines, "tool "+name+" ["+status+"]: "+clipLine(toolResultText(d.Part.ToolResult.Content), councilActionCap))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > k {
		lines = lines[len(lines)-k:]
	}
	return "- " + strings.Join(lines, "\n- ")
}

// fmtElapsed renders a duration coarsely (seconds under a minute, else Xm or XhYm)
// — a pacing signal, not a stopwatch display.
func fmtElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// normEq reports whether two answers are the same modulo whitespace — the
// cheap, deterministic notion of "the agent resubmitted its rejected answer".
func normEq(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// clipLine returns at most n bytes of s (rune-safe) with an ellipsis, keeping a single
// evidence bullet on one line (no marker/newline reintroduced).
func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// clipSpec bounds an authoritative "follow VERBATIM" spec at n bytes (rune-safe).
// Unlike clipLine it does NOT append a bare "…": a delegate told to reproduce exact
// identifiers can otherwise copy the dangling ellipsis into an edit old-string (or an
// output the grader checks), matching nothing. When it truncates it appends an explicit
// marker on its own line so the model knows the cutoff is not part of the spec.
func clipSpec(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[…spec truncated here — this cutoff is NOT part of the spec; if you need an exact value beyond this point, ask for the remainder rather than reproducing this line]"
}

// toolResultText renders a tool result's JSON content as readable one-ish-line text
// (unwrapping a JSON string, collapsing newlines) for the council evidence summary.
func toolResultText(raw json.RawMessage) string {
	s := string(raw)
	var str string
	if json.Unmarshal(raw, &str) == nil {
		s = str
	}
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " ⏎ "))
}

// truncateForCouncil clips s to at most n bytes (on a rune boundary), appending a
// marker when truncated.
func truncateForCouncil(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…[diff truncated]"
}

// tailForCouncil keeps at most the last n bytes of s (on a rune boundary), since a
// failing build/test puts the useful output last.
func tailForCouncil(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "…[earlier output truncated]\n" + s[cut:]
}

// runCouncilGate runs the consensus termination gate (D14) at top level. It
// returns true when the council voted to CONTINUE — it has injected the
// aggregated feedback as a system prompt, so the caller should loop again. It
// returns false when the turn may finish: the council voted done, rounds were
// exhausted, the round made no progress, or the gate errored.
//
// Safety (so the council can never trap the loop): rounds are capped, repeated or
// empty feedback stops the gate, and any deliberation error finishes the turn.
func (a *App) runCouncilGate(ctx context.Context, s session.Session, agent AgentSpec, turnTask, lastText string, rounds *int, lastFeedback *string, changes string, stepsLeft int, fabrication string, turnElapsed time.Duration, spent *time.Duration, deadlocked *bool) bool {
	// deadlocked reports (to the caller) whether this finish is a genuine round-cap
	// deadlock — the council used its whole budget and never approved — as opposed to
	// an approval or a cost-capped finish. Only the round-cap branch sets it, so a
	// stuck-recovery hook can distinguish "held unmet for every round" from a DONE vote
	// that happened to land on the last allowed round (identical by rounds count alone).
	if deadlocked != nil {
		*deadlocked = false
	}
	// An interrupt mid-finish must not trigger a deliberation or inject a spurious
	// feedback prompt — let the loop unwind the cancellation.
	if ctx.Err() != nil {
		return false
	}
	sid := s.ID
	councilActor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	a.setStage(sid, stageCouncil) // tag deliberation events as the council stage (D15)

	maxRounds := a.cfg.CouncilMaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}
	if *rounds >= maxRounds {
		// Round cap hit — finish (a normal outcome, not an error), but say plainly
		// that the council never approved: a deadlocked result must read as
		// UNVERIFIED, not as a done the members agreed to. Carry the last feedback
		// so the record shows WHAT was still unmet. Recorded on a fresh round number
		// so it doesn't collide with the last deliberated round.
		note := fmt.Sprintf("unresolved after %d rounds — finishing; the council never approved this result, treat it as UNVERIFIED", maxRounds)
		if fb := strings.TrimSpace(*lastFeedback); fb != "" {
			note += "; still unmet: " + clipLine(fb, 200)
		}
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: *rounds + 1, Decision: string(council.Done),
			Note: note,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		if deadlocked != nil {
			*deadlocked = true
		}
		return false
	}
	// Cost-efficiency cap (self-measured, no external info): when deliberation has
	// already eaten a disproportionate share of the turn's own wall clock — a slow
	// backend polling 3 members per round — further rounds cost more than they
	// return. At least one round always runs; the cap only stops round 2+.
	if *rounds >= 1 && *spent >= 60*time.Second && *spent*4 >= turnElapsed {
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: *rounds + 1, Decision: string(council.Done),
			Note: fmt.Sprintf("deliberation has cost %s of a %s turn — finishing instead of another round; treat as UNVERIFIED",
				fmtElapsed(*spent), fmtElapsed(turnElapsed)),
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		return false
	}
	*rounds++

	members := a.cfg.CouncilMembers
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	rule := a.cfg.CouncilRule
	if rule == "" {
		rule = council.DefaultRule
	}

	// Evidence: the user's goal (Task), the agent's final message (Report/claim),
	// and the working diff. Plan (acceptance criteria) and Signals are D15/D16.
	// Task is the LATEST genuine user instruction, not the first — in a multi-turn
	// session a refinement ("now add X") must be judged against itself, else the
	// council holds every turn to the opening prompt and rejects correct follow-up
	// work (and the agent then "fixes" it by undoing what the user just asked for).
	// turnTask was snapshotted at the turn's first step (not re-read here), so a steer
	// that arrives during deliberation can't swap what the council judges against.
	evs, _ := a.store.Read(ctx, sid, 0)
	task := turnTask
	if task == "" { // defensive: fall back to the latest genuine prompt
		task = lastUserPromptText(evs)
	}
	changes = truncateForCouncil(changes, councilDiffCap) // the agent's before→after edits, bounded
	// The agent's plan (todos, with status) is the council's CONTRACT (D15): the
	// completeness lens judges the report/diff against it, and any still-pending
	// item is strong grounds to continue. Empty when the agent kept no plan.
	plan := ""
	if td := a.Todos(sid); len(td) > 0 {
		plan = formatTodos(td)
	}
	// Acceptance criteria as the contract (D15/D17). The plan-audit council may have
	// already derived them this turn — those are ALWAYS used (plan turns). Otherwise,
	// only when opted in (`[council] criteria`), elicit them from the task. Prepended
	// so the council judges "done" against the contract.
	crit := a.cachedCriteria(s.ID)
	if crit == "" && a.cfg.CouncilCriteria {
		crit = a.acceptanceCriteria(ctx, agent, s, task)
	}
	if crit != "" {
		if plan != "" {
			plan = "Acceptance criteria:\n" + crit + "\n\nPlan (todos):\n" + plan
		} else {
			plan = "Acceptance criteria:\n" + crit
		}
	}

	// Opt-in deterministic evidence (D16): run each configured signal command and
	// feed its outcome to the council, so members judge on proof, not just claims.
	var signals []port.Signal
	var signalSummaries []string
	// Always-on deterministic signal: the agent changed a deliverable this turn but ran no
	// command exercising the current version (see runGuard.unverifiedDeliverable). Unlike the
	// opt-in command signals below, this needs no config — it is derived structurally from the
	// tool log the council is already judging against, and it is language-agnostic (it replaced
	// the old English confession-phrase scan that missed non-English or non-confessing fakes).
	if strings.TrimSpace(fabrication) != "" {
		signals = append(signals, port.Signal{Source: "self-check", Kind: "unverified", Status: "fail", Detail: tailForCouncil(fabrication, councilSignalCap)})
		signalSummaries = append(signalSummaries, "self-check: unverified")
	}
	if a.plat != nil {
		for _, sp := range a.cfg.CouncilSignals {
			if ctx.Err() != nil {
				break // interrupted — stop spawning further checks
			}
			if strings.TrimSpace(sp.Command) == "" {
				continue
			}
			name := sp.Name
			if name == "" {
				name = "check"
			}
			out, code := a.runVerifyCmd(ctx, s.Workdir, sp.Command)
			status := "pass"
			if code != 0 {
				status = "fail"
			}
			signals = append(signals, port.Signal{Source: name, Kind: "check", Status: status, Detail: tailForCouncil(out, councilSignalCap)})
			signalSummaries = append(signalSummaries, name+": "+status)
		}
	}
	// Cancellation during GitDiff/verify: unwind rather than persist a misleading
	// convened fact or deliberate on partial evidence.
	if ctx.Err() != nil {
		return false
	}

	// Tell the council when the turn changed nothing (no agent file edits, no signals): a
	// pure read-only / investigation / answer turn has no artifact to verify, so members
	// judge the report on its merits instead of demanding edits that were never going to
	// exist — the consensus rule is unchanged.
	noChanges := strings.TrimSpace(changes) == "" && len(signals) == 0

	labels := make([]string, len(members))
	for i, m := range members {
		labels[i] = m.Name
	}
	cd, _ := json.Marshal(event.CouncilConvenedData{
		Round: *rounds, Members: labels, Rule: string(rule), Signals: signalSummaries,
		Task: task, Plan: plan, Report: lastText, Changes: changes, NoChanges: noChanges,
	})
	a.appendFact(ctx, sid, event.TypeCouncilConvened, councilActor, cd)
	// Live panel: announce which members are deliberating this round.
	for _, m := range members {
		ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: *rounds, Member: m.Name, State: "asking"})
		a.publishTransient(sid, event.TypeCouncilDeliberating, councilActor, ld)
	}

	delibStart := time.Now()
	delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
		Round:        *rounds,
		Task:         task,
		Plan:         plan,
		Report:       lastText,
		Actions:      turnToolEvidence(evs, councilActionsCap),
		Signals:      signals,
		Changes:      changes,
		NoChanges:    noChanges,
		Members:      members,
		Rule:         rule,
		DefaultModel: s.Model.Model,
		StepsLeft:    stepsLeft,
	})
	*spent += time.Since(delibStart)
	if err != nil {
		// A gate failure must not trap the turn — record it as a forced finish
		// (a note, not an error event, since the turn completes normally).
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: *rounds, Decision: string(council.Done), Note: "council unavailable: " + err.Error(),
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		return false
	}
	// An interrupt during deliberation: unwind rather than inject feedback.
	if ctx.Err() != nil {
		return false
	}

	for _, v := range delib.Verdicts {
		vd, _ := json.Marshal(event.CouncilVerdictData{
			Round: *rounds, Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
			Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback,
		})
		a.appendFact(ctx, sid, event.TypeCouncilVerdict, councilActor, vd)
	}
	emitDecided := func(decision council.Decision, feedback, note string) {
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: *rounds, Decision: string(decision), Tally: delib.Breakdown, Feedback: feedback, Note: note,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
	}

	if delib.Decision != council.Continue {
		emitDecided(council.Done, "", "")
		return false // the council agrees the turn may finish
	}

	// No-progress guard: empty or repeated feedback means another round would just
	// spin, so finish instead — recorded as a forced "done", not an error.
	fb := strings.TrimSpace(delib.Feedback)
	if fb == "" || fb == *lastFeedback {
		emitDecided(council.Done, "", "members voted continue but gave no new feedback — finishing; the council never approved this result, treat it as UNVERIFIED")
		return false
	}
	*lastFeedback = fb

	emitDecided(council.Continue, fb, "")
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: "Council review (not user input) — the task is not yet done:\n" + fb}},
	})
	a.appendFact(ctx, sid, event.TypePromptSubmitted, councilActor, pd)
	return true
}
