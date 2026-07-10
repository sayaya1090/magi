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

// concernPremiseKey is the stable ledger Key for the N14 unverified-premise concern. It
// equals the fresh signal's "source/kind", so the ledger merge dedups a concern that
// already fired this turn against the one carried from an earlier turn.
const concernPremiseKey = "self-check/unverified-premise"

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

// knowledgeLookupTools are the tools whose whole job is to fetch an EXTERNAL FACT the
// agent does not already possess. A failure here that the agent does not recover from
// is the N14 "research dead-end" fabrication branch: the agent fills the gap with a
// guessed premise (e.g. a restriction-enzyme site, an API detail, a constant) and
// proceeds. The execution-evidence gate (runGuard.unverifiedDeliverable, structural,
// about the deliverable existing/being exercised) is blind to it because execution
// succeeds — the lie is in a FACT, not the artifact — and an LLM council cannot verify
// a domain fact from reasoning alone.
var knowledgeLookupTools = map[string]bool{
	"websearch":  true,
	"web_search": true,
	"webfetch":   true,
	"web_fetch":  true,
	"fetch":      true,
}

// unverifiedLookup scans the LATEST turn and returns a non-empty detail when a
// knowledge-lookup tool failed and NO lookup in the turn succeeded — i.e. the agent may
// have proceeded on an unverified external premise. It returns "" when there was no
// failed lookup, or when any lookup succeeded (the agent plausibly recovered a fact).
// Recovery is judged turn-wide, not per-fact: a single successful lookup silences the
// signal even if it answered a different question — a deliberate bias toward silence to
// keep the signal from churning; it under-fires rather than over-fires.
//
// It is deliberately structural and language-agnostic, mirroring
// runGuard.unverifiedDeliverable, and — crucially — it resurfaces a failure that
// turnToolEvidence's most-recent-k window would otherwise age out (the failed lookup
// happens early; the deliverable's format checks happen last), so the council would
// never see the un-verified premise without this signal. Advisory, not a veto: it makes
// the council look harder, exactly like the self-check "unverified" fabrication signal.
func unverifiedLookup(evs []event.Event) string {
	names := map[string]string{} // callID -> tool name
	var failed []string          // "tool: err snippet" for each un-recovered failed lookup
	anySuccess := false          // a lookup returned without error → plausible recovery
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted { // new turn boundary → judge only the latest turn
			names = map[string]string{}
			failed = nil
			anySuccess = false
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
			r := d.Part.ToolResult
			if r == nil || !knowledgeLookupTools[names[r.CallID]] {
				continue
			}
			if r.IsError {
				failed = append(failed, names[r.CallID]+": "+clipLine(toolResultText(r.Content), councilActionCap))
			} else {
				anySuccess = true
			}
		}
	}
	if len(failed) == 0 || anySuccess {
		return ""
	}
	return "a knowledge lookup failed this turn and no lookup succeeded — any external fact the agent " +
		"went on to use (an API detail, constant, sequence, name, spec) may be an UNVERIFIED guess rather than a " +
		"confirmed value. If the deliverable depends on such a fact, its correctness is unproven:\n- " +
		strings.Join(failed, "\n- ")
}

// lookupRecovered reports whether a knowledge lookup SUCCEEDED in the latest turn — the
// only POSITIVE evidence that an unverified-premise concern is actually resolved. It is
// deliberately distinct from "unverifiedLookup returned empty": empty also covers a turn
// with no lookup at all, and mere absence must NEVER auto-resolve a still-open concern
// (that would let a quiet turn launder away a premise that was never verified). Only a
// real, successful lookup clears it.
func lookupRecovered(evs []event.Event) bool {
	names := map[string]string{}
	recovered := false
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted { // judge only the latest turn
			names = map[string]string{}
			recovered = false
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
			r := d.Part.ToolResult
			if r != nil && knowledgeLookupTools[names[r.CallID]] && !r.IsError {
				recovered = true
			}
		}
	}
	return recovered
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

// councilInput is the read-only evidence one council round judges: the turn's
// task and the agent's latest report/diff, plus the self-measured turn clock,
// remaining step budget, and any structural fabrication signal. Snapshotted by
// the caller so a steer mid-deliberation can't swap what the council judges.
type councilInput struct {
	turnTask    string        // the goal the council judges "done" against (turn-snapshotted)
	lastText    string        // the agent's final message this step (the claim under review)
	changes     string        // the agent's before→after edits (bounded)
	fabrication string        // structural unverified-deliverable signal, "" when none
	stepsLeft   int           // remaining step budget, surfaced to members
	turnElapsed time.Duration // the turn's own wall clock, for the cost-efficiency cap
}

// councilTurn is the per-turn accounting the consensus gate carries ACROSS rounds:
// rounds run, the last round's feedback (no-progress detection), wall-clock spent
// deliberating (cost cap), and whether a finish was a genuine round-cap deadlock.
// The loop owns one per turn (zeroed on reground) and passes a pointer so each
// round updates it in place — replacing the four in/out pointer params it grew from.
type councilTurn struct {
	rounds     int           // consensus gate rounds this turn (D14)
	feedback   string        // last round's feedback (no-progress detection)
	spent      time.Duration // self-measured wall-clock consumed by deliberations
	deadlocked bool          // set iff a finish was a genuine round-cap deadlock (never approved)
}

// runCouncilGate runs the consensus termination gate (D14) at top level. It
// returns true when the council voted to CONTINUE — it has injected the
// aggregated feedback as a system prompt, so the caller should loop again. It
// returns false when the turn may finish: the council voted done, rounds were
// exhausted, the round made no progress, or the gate errored.
//
// Safety (so the council can never trap the loop): rounds are capped, repeated or
// empty feedback stops the gate, and any deliberation error finishes the turn.
// Returns keepWorking (true → the turn must run another step) and, when the turn
// is allowed to FINISH (keepWorking false), an unverifiedReason: non-empty means
// the council never actually approved this result (round-cap deadlock, cost-cap,
// no-new-feedback, or an unavailable council) so the finish is UNVERIFIED, not a
// genuine done. Empty reason on a finish = a real approval (or a cancel the loop
// unwinds separately). The caller propagates the reason into turn.finished so the
// UI does not paint an abandoned task as a confident "done".
func (a *App) runCouncilGate(ctx context.Context, s session.Session, agent AgentSpec, in councilInput, ct *councilTurn) (keepWorking bool, unverifiedReason string) {
	// ct.deadlocked reports (to the caller) whether this finish is a genuine round-cap
	// deadlock — the council used its whole budget and never approved — as opposed to
	// an approval or a cost-capped finish. Only the round-cap branch sets it, so a
	// stuck-recovery hook can distinguish "held unmet for every round" from a DONE vote
	// that happened to land on the last allowed round (identical by rounds count alone).
	ct.deadlocked = false
	// An interrupt mid-finish must not trigger a deliberation or inject a spurious
	// feedback prompt — let the loop unwind the cancellation.
	if ctx.Err() != nil {
		return false, ""
	}
	sid := s.ID
	councilActor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	a.setStage(sid, stageCouncil) // tag deliberation events as the council stage (D15)

	maxRounds := a.cfg.CouncilMaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}
	if ct.rounds >= maxRounds {
		// Round cap hit — finish (a normal outcome, not an error), but say plainly
		// that the council never approved: a deadlocked result must read as
		// UNVERIFIED, not as a done the members agreed to. Carry the last feedback
		// so the record shows WHAT was still unmet. Recorded on a fresh round number
		// so it doesn't collide with the last deliberated round.
		note := fmt.Sprintf("unresolved after %d rounds — finishing; the council never approved this result, treat it as UNVERIFIED", maxRounds)
		if fb := strings.TrimSpace(ct.feedback); fb != "" {
			note += "; still unmet: " + clipLine(fb, 200)
		}
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ct.rounds + 1, Decision: string(council.Done),
			Note: note, Forced: true,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		ct.deadlocked = true
		return false, fmt.Sprintf("the council never approved this result within %d round(s)", maxRounds)
	}
	// Cost-efficiency cap (self-measured, no external info): when deliberation has
	// already eaten a disproportionate share of the turn's own wall clock — a slow
	// backend polling 3 members per round — further rounds cost more than they
	// return. At least one round always runs; the cap only stops round 2+.
	if ct.rounds >= 1 && ct.spent >= 60*time.Second && ct.spent*4 >= in.turnElapsed {
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ct.rounds + 1, Decision: string(council.Done),
			Note: fmt.Sprintf("deliberation has cost %s of a %s turn — finishing instead of another round; treat as UNVERIFIED",
				fmtElapsed(ct.spent), fmtElapsed(in.turnElapsed)),
			Forced: true,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		return false, "deliberation was cost-capped before the council approved"
	}
	ct.rounds++

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
	// Mask EVERY interjection detected this turn: each is a PromptSubmitted spliced
	// mid-turn, and the evidence scanners below (turnToolEvidence/unverifiedLookup/
	// lookupRecovered) treat every PromptSubmitted as a turn boundary — so an un-masked
	// interjection would reset their window and make the council judge only the fragment
	// after it. taskEvents (not liveEvents) so an interjection answered inline during a
	// background dispatch — never queued — is still hidden from the council.
	evs, _ := a.store.Read(ctx, sid, 0)
	evs = a.taskEvents(sid, evs)
	task := in.turnTask
	if task == "" { // defensive: fall back to the latest genuine prompt
		task = lastUserPromptText(evs)
	}
	changes := truncateForCouncil(in.changes, councilDiffCap) // the agent's before→after edits, bounded
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
	if strings.TrimSpace(in.fabrication) != "" {
		signals = append(signals, port.Signal{Source: "self-check", Kind: "unverified", Status: "fail", Detail: tailForCouncil(in.fabrication, councilSignalCap)})
		signalSummaries = append(signalSummaries, "self-check: unverified")
	}
	// Always-on deterministic signal (N14): a knowledge lookup failed this turn and was
	// never recovered, so a fact the deliverable rests on may be a guessed premise. This
	// resurfaces a failure that turnToolEvidence's recency window ages out before the
	// council convenes — the research-dead-end fabrication branch the execution-evidence
	// gate above cannot see (execution succeeds; the lie is in a fact, not the artifact).
	lp := unverifiedLookup(evs)
	if lp != "" {
		signals = append(signals, port.Signal{Source: "self-check", Kind: "unverified-premise", Status: "fail", Detail: tailForCouncil(lp, councilSignalCap)})
		signalSummaries = append(signalSummaries, "self-check: unverified-premise")
	}
	// Persist the premise concern to the durable ledger so it survives BEYOND this turn:
	// the fresh signal above only fires the turn the lookup failed, but the fact the
	// deliverable rests on stays unverified until it is actually looked up. Raise it
	// (idempotently — only when not already open) when it fires; auto-resolve ONLY on
	// positive recovery (a lookup succeeded this turn), never on mere absence. Absence must
	// leave a still-true concern open — that is what stops a quiet turn (or a reset) from
	// laundering an unverified premise into an approval. sessionConcerns here folds the
	// PRE-round log; the resolve/raise below then updates it for the merge that follows.
	premiseOpen := false
	for _, c := range sessionConcerns(evs) {
		if c.Key == concernPremiseKey {
			premiseOpen = true
			break
		}
	}
	switch {
	case lp != "" && !premiseOpen:
		_ = a.appendConcernRaised(ctx, sid, councilActor, concernPremiseKey, "self-check", "unverified-premise", "fail", tailForCouncil(lp, councilSignalCap), "")
	case premiseOpen && lookupRecovered(evs):
		_ = a.appendConcernResolved(ctx, sid, councilActor, concernPremiseKey, "auto", "a knowledge lookup succeeded this turn")
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
		return false, ""
	}

	// Merge the open ledger into this round's signals: carry a concern the council would
	// otherwise not see — one raised on an EARLIER turn (cross-turn survival) or bubbled up
	// from a SUBAGENT (cross-boundary). Dedup by Key against the freshly computed signals so
	// a concern that already fired THIS turn is not listed twice, while a still-open concern
	// that did NOT fire this turn is surfaced from the ledger. Re-read the log so this turn's
	// own raise/resolve above are reflected (a just-resolved concern is not re-carried).
	ledgerEvs, _ := a.store.Read(ctx, sid, 0)
	present := map[string]bool{}
	for _, sg := range signals {
		present[sg.Source+"/"+sg.Kind] = true
	}
	for _, c := range sessionConcerns(ledgerEvs) {
		if present[c.Key] {
			continue
		}
		signals = append(signals, c.Signal())
		signalSummaries = append(signalSummaries, c.Source+": "+c.Kind+" (carried)")
		present[c.Key] = true
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
		Round: ct.rounds, Members: labels, Rule: string(rule), Signals: signalSummaries,
		Task: task, Plan: plan, Report: in.lastText, Changes: changes, NoChanges: noChanges,
	})
	a.appendFact(ctx, sid, event.TypeCouncilConvened, councilActor, cd)
	// Live panel: announce which members are deliberating this round.
	for _, m := range members {
		ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: ct.rounds, Member: m.Name, State: "asking"})
		a.publishTransient(sid, event.TypeCouncilDeliberating, councilActor, ld)
	}

	delibStart := time.Now()
	delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
		Round:        ct.rounds,
		Task:         task,
		Plan:         plan,
		Report:       in.lastText,
		Actions:      turnToolEvidence(evs, councilActionsCap),
		Signals:      signals,
		Changes:      changes,
		NoChanges:    noChanges,
		Members:      members,
		Rule:         rule,
		DefaultModel: s.Model.Model,
		StepsLeft:    in.stepsLeft,
	})
	ct.spent += time.Since(delibStart)
	if err != nil {
		// A gate failure must not trap the turn — record it as a forced finish
		// (a note, not an error event, since the turn completes normally).
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ct.rounds, Decision: string(council.Done), Note: "council unavailable: " + err.Error(), Forced: true,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		return false, "the council was unavailable, so the result was not approved: " + err.Error()
	}
	// An interrupt during deliberation: unwind rather than inject feedback.
	if ctx.Err() != nil {
		return false, ""
	}

	for _, v := range delib.Verdicts {
		vd, _ := json.Marshal(event.CouncilVerdictData{
			Round: ct.rounds, Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
			Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback,
		})
		a.appendFact(ctx, sid, event.TypeCouncilVerdict, councilActor, vd)
	}
	emitDecided := func(decision council.Decision, feedback, note string, forced bool) {
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ct.rounds, Decision: string(decision), Tally: delib.Breakdown, Feedback: feedback, Note: note, Forced: forced,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
	}

	if delib.Decision != council.Continue {
		emitDecided(council.Done, "", "", false)
		return false, "" // the council agrees the turn may finish — a genuine, verified done
	}

	// No-progress guard: empty or repeated feedback means another round would just
	// spin, so finish instead — recorded as a forced "done", not an error.
	fb := strings.TrimSpace(delib.Feedback)
	if fb == "" || fb == ct.feedback {
		emitDecided(council.Done, "", "members voted continue but gave no new feedback — finishing; the council never approved this result, treat it as UNVERIFIED", true)
		return false, "the council voted to continue but produced no actionable feedback, so the result stands unapproved"
	}
	ct.feedback = fb

	// Rung 1 (stuck-concern escalation, opt-in): once the council has held the turn open
	// for councilMeansRound+ rounds, the bare objection has demonstrably failed to move the
	// agent — append a concrete, task-agnostic recipe for satisfying it (how to keep a
	// process alive, how to produce real evidence), keyed off the feedback's own words. The
	// hint rides only on the injected prompt; lastFeedback and the recorded decision keep the
	// RAW feedback, so no-progress repeat-detection is unaffected and the gate is never
	// weakened (augment, not approve). See docs/proposals/council-stuck-concern-escalation.md.
	inject := fb
	if councilMeansEnabled() && ct.rounds >= councilMeansRound {
		if hint := meansHint(fb); hint != "" {
			inject = fb + "\n\n" + hint
		}
	}

	emitDecided(council.Continue, fb, "", false)
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: "Council review (not user input) — the task is not yet done:\n" + inject}},
	})
	a.appendFact(ctx, sid, event.TypePromptSubmitted, councilActor, pd)
	return true, ""
}
