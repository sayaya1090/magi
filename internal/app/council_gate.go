package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	councilActionsCap = 8    // most recent turn outputs (model text + tool results) shown to the council
	councilActionCap  = 4000 // per-item byte cap — 400 was far too tight: it clipped a file/output mid-content
	// (e.g. a `cat script.py` whose bug was past byte 400), so the council could see a wrong
	// RESULT but not the CAUSE, and its feedback stayed symptom-level round after round. Kept in
	// the same ballpark as councilDiffCap so a whole small file/output is visible, not a fragment.
	maxSubagentsShown  = 6 // most recent this-turn subagents whose evidence is surfaced to the council
	subagentActionsCap = 6 // most recent tool results per subagent shown to the council
)

// concernPremiseKey is the stable ledger Key for the N14 unverified-premise concern. It
// equals the fresh signal's "source/kind", so the ledger merge dedups a concern that
// already fired this turn against the one carried from an earlier turn.
const concernPremiseKey = "self-check/unverified-premise"

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

// councilInput is the read-only evidence one council round judges: the turn's
// task and the agent's latest report/diff, plus the self-measured turn clock,
// remaining step budget, and any structural fabrication signal. Snapshotted by
// the caller so a steer mid-deliberation can't swap what the council judges.
type councilInput struct {
	turnTask    string        // the goal the council judges "done" against (turn-snapshotted)
	lastText    string        // the agent's final message this step (the claim under review)
	changes     string        // the agent's before→after edits (bounded)
	fabrication string        // structural unverified-deliverable signal, "" when none
	checkLedger string        // failing deliverable-check results (executed, not narrated), "" when none
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
	// Round-cost reduction state (set at each rejection):
	rejectToolCalls int               // total tool calls at the last rejection — a re-finish with the same count changed nothing
	rejectEvs       int               // event-log length at the last rejection (delta-evidence boundary for the re-round)
	prevVerdicts    []council.Verdict // last round's verdicts (done votes carry into a focused re-round)
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
// councilKeepWork rides on every done-gate rejection. A rejection tends to trigger a
// weak model's "start over" reflex — rm -rf the finished artifacts and rebuild from
// scratch — which destroys working state, recharges churn, and leaves nothing standing
// (not even a required server process) when the wall clock expires. The council asked
// for EVIDENCE, not a rebuild; say so explicitly at the moment of rejection.
const councilKeepWork = "\n\nWhat is missing is EVIDENCE, not a rebuild: do NOT delete, recreate, or rebuild " +
	"work that already exists — destroying finished artifacts loses progress and does not address the feedback. " +
	"Take the SMALLEST action that produces the requested evidence against the CURRENT state (run the real " +
	"command or test and show its output, keep required processes running)."

// councilCompletionAudit rides on every done-gate rejection, after the round's
// feedback. Over a long turn a weak model drifts: it paraphrases the spec (losing
// verbatim identifiers a grader checks), narrows success to the easiest passing
// subset, and reports "done" from intent rather than evidence. The audit re-anchors
// the turn on the FULL objective and forces completion to be proven requirement by
// requirement against the current state — not merely consistent with completion.
const councilCompletionAudit = "\n\nCompletion audit — treat 'done' as UNPROVEN until you verify it:\n" +
	"- Keep the FULL objective. Do not redefine success as a smaller, easier, or merely-compatible task, and do not paraphrase the spec — preserve its exact identifiers, names, and values verbatim.\n" +
	"- Derive every explicit requirement, named artifact, command, and test from the objective, and for each one inspect the authoritative current-state evidence (file contents, command output, test result) that would prove it.\n" +
	"- Match the check's scope to the requirement's scope; a narrow check does not prove a broad claim.\n" +
	"- Treat uncertain, indirect, or missing evidence as NOT done — gather stronger evidence or keep working. Do not rely on intent, partial progress, or a plausible-looking answer as proof."

// continuationText assembles the prompt injected when the council votes CONTINUE:
// the round's feedback, the frozen acceptance contract (when the plan-audit froze
// executable deliverable checks — the review may only judge against these, never add
// new scope), then a verbatim re-anchor of the objective (so the agent cannot lose the
// exact spec over a long turn), then the completion-audit rubric.
func continuationText(inject, task, contract string) string {
	var b strings.Builder
	b.WriteString("Council review (not user input) — the task is not yet done:\n")
	b.WriteString(inject)
	if c := strings.TrimSpace(contract); c != "" {
		b.WriteString(c)
	}
	b.WriteString(councilKeepWork)
	if t := strings.TrimSpace(task); t != "" {
		b.WriteString("\n\nOriginal objective (verbatim — pursue this exact end state, do not narrow or paraphrase it):\n")
		b.WriteString(clipLine(t, councilDiffCap))
	}
	b.WriteString(councilCompletionAudit)
	return b.String()
}

// councilPreRoundCaps applies the two finish-without-deliberating caps before a round is
// spent, returning (unverifiedReason, done): done=true means the gate must finish now, always
// UNVERIFIED (the council never approved). The round cap (deadlock) additionally sets
// ct.deadlocked so a stuck-recovery hook can distinguish "held unmet for every round" from a
// DONE vote that merely landed on the last allowed round. The cost cap stops round 2+ once
// deliberation has eaten a disproportionate share of the turn's own wall clock (a slow backend
// polling members) — at least one round always runs. done=false lets the caller convene.
func (a *App) councilPreRoundCaps(ctx context.Context, sid session.SessionID, in councilInput, ct *councilTurn, maxRounds int, councilActor event.Actor) (string, bool) {
	if ct.rounds >= maxRounds {
		// Round cap hit — finish (a normal outcome, not an error), but say plainly that the
		// council never approved: a deadlocked result must read as UNVERIFIED, not as a done the
		// members agreed to. Carry the last feedback so the record shows WHAT was still unmet.
		// Recorded on a fresh round number so it doesn't collide with the last deliberated round.
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
		return fmt.Sprintf("the council never approved this result within %d round(s)", maxRounds), true
	}
	// Cost-efficiency cap (self-measured, no external info): when deliberation has already eaten
	// a disproportionate share of the turn's own wall clock — a slow backend polling 3 members
	// per round — further rounds cost more than they return. At least one round always runs; the
	// cap only stops round 2+.
	if ct.rounds >= 1 && ct.spent >= 60*time.Second && ct.spent*4 >= in.turnElapsed {
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ct.rounds + 1, Decision: string(council.Done),
			Note: fmt.Sprintf("deliberation has cost %s of a %s turn — finishing instead of another round; treat as UNVERIFIED",
				fmtElapsed(ct.spent), fmtElapsed(in.turnElapsed)),
			Forced: true,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		return "deliberation was cost-capped before the council approved", true
	}
	return "", false
}

// councilSignals assembles the deterministic evidence this round's members judge on, returning
// the signals and their one-line summaries. Two always-on structural signals need no config:
// unverified execution (a deliverable changed but never exercised, in.fabrication) and an
// unverified premise (a knowledge lookup that failed this turn and was never recovered, N14) —
// the latter is also persisted to the durable concern ledger so it survives beyond this turn
// (raised idempotently, auto-resolved ONLY on a positive lookup recovery, never on mere
// absence). Then the opt-in [council] signal commands run (D16). Finally the open ledger is
// merged in, deduped by key, so a concern raised on an earlier turn or bubbled up from a
// subagent is carried even when it did not fire this turn.
func (a *App) councilSignals(ctx context.Context, s session.Session, evs []event.Event, fabrication, checkLedger string, councilActor event.Actor) ([]port.Signal, []string) {
	sid := s.ID
	var signals []port.Signal
	var signalSummaries []string
	// Executed deliverable-check ledger (MAGI_STEP_VERIFY): the plan's checks were RUN and some
	// failed. A hard FAILING signal the council must honor over the agent's "I'm done" claim —
	// real command output, not narration. This is the ledger the whole gate exists for.
	if strings.TrimSpace(checkLedger) != "" {
		signals = append(signals, port.Signal{Source: "deliverable-check", Kind: "contract", Status: "fail", Detail: tailForCouncil(checkLedger, councilSignalCap)})
		signalSummaries = append(signalSummaries, "deliverable-check: FAILED")
	}
	// Always-on deterministic signal: the agent changed a deliverable this turn but ran no
	// command exercising the current version (see runGuard.unverifiedDeliverable). Unlike the
	// opt-in command signals below, this needs no config — it is derived structurally from the
	// tool log the council is already judging against, and it is language-agnostic (it replaced
	// the old English confession-phrase scan that missed non-English or non-confessing fakes).
	if strings.TrimSpace(fabrication) != "" {
		signals = append(signals, port.Signal{Source: "self-check", Kind: "unverified", Status: "fail", Detail: tailForCouncil(fabrication, councilSignalCap)})
		signalSummaries = append(signalSummaries, "self-check: unverified")
	}
	// Always-on deterministic signal (N14): a knowledge lookup failed this turn and was never
	// recovered, so a fact the deliverable rests on may be a guessed premise. This resurfaces a
	// failure that turnToolEvidence's recency window ages out before the council convenes — the
	// research-dead-end fabrication branch the execution-evidence gate cannot see (execution
	// succeeds; the lie is in a fact, not the artifact).
	lp := unverifiedLookup(evs)
	if lp != "" {
		signals = append(signals, port.Signal{Source: "self-check", Kind: "unverified-premise", Status: "fail", Detail: tailForCouncil(lp, councilSignalCap)})
		signalSummaries = append(signalSummaries, "self-check: unverified-premise")
	}
	// Persist the premise concern to the durable ledger so it survives BEYOND this turn: the
	// fresh signal above only fires the turn the lookup failed, but the fact the deliverable
	// rests on stays unverified until it is actually looked up. Raise it (idempotently — only
	// when not already open) when it fires; auto-resolve ONLY on positive recovery (a lookup
	// succeeded this turn), never on mere absence. Absence must leave a still-true concern open —
	// that is what stops a quiet turn (or a reset) from laundering an unverified premise into an
	// approval. sessionConcerns here folds the PRE-round log; the resolve/raise below then
	// updates it for the merge that follows.
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
	// Merge the open ledger into this round's signals: carry a concern the council would
	// otherwise not see — one raised on an EARLIER turn (cross-turn survival) or bubbled up from
	// a SUBAGENT (cross-boundary). Dedup by Key against the freshly computed signals so a concern
	// that already fired THIS turn is not listed twice, while a still-open concern that did NOT
	// fire this turn is surfaced from the ledger. Re-read the log so this turn's own raise/resolve
	// above are reflected (a just-resolved concern is not re-carried).
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
	return signals, signalSummaries
}

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
	if reason, done := a.councilPreRoundCaps(ctx, sid, in, ct, maxRounds, councilActor); done {
		return false, reason
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

	// No-progress delta gate (round-cost reduction 1): a re-finish with ZERO new
	// tool actions since the last rejection cannot change any evidence-based
	// verdict — deliberating again only burns wall clock (the completion-loop
	// pattern paid this every round). Reuse the standing rejection without
	// convening; the reused round still counts, so a persistent re-declarer
	// reaches the round cap (and its UNVERIFIED landing) sooner, not later.
	if ct.rounds >= 2 && len(ct.prevVerdicts) > 0 && countToolCalls(evs) == ct.rejectToolCalls {
		if ct.rounds >= maxRounds {
			note := fmt.Sprintf("unresolved after %d rounds — finishing; the council never approved this result, treat it as UNVERIFIED; still unmet: %s",
				maxRounds, clipLine(ct.feedback, 200))
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: ct.rounds, Decision: string(council.Done), Note: note, Forced: true,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
			ct.deadlocked = true
			return false, fmt.Sprintf("the council never approved this result within %d round(s)", maxRounds)
		}
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ct.rounds, Decision: string(council.Continue), Feedback: ct.feedback,
			Note: "no new tool actions since the last rejection — verdict reused without deliberation",
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		pd, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: "m_" + newID(),
			Parts: []session.Part{{Kind: session.PartText, Text: "Council review (not user input) — you re-declared " +
				"completion WITHOUT taking any new action since the last rejection; the previous feedback still stands:\n" +
				ct.feedback + councilKeepWork}},
		})
		a.appendFact(ctx, sid, event.TypePromptSubmitted, councilActor, pd)
		return true, ""
	}

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
	// The mined identifier/type requirements (specmine) are a soft contract the
	// executor received before implementing. Show them to the members too: without
	// this, an implementation that ignores the note's recommended standard construct
	// and hand-rolls the mechanism (observed: the note named the construct, the code
	// re-cancelled tasks mid-cleanup) presents no visible deviation for the council
	// to question — the judges never saw the recommendation the executor got.
	if mined := a.cachedSpecMine(s.ID); mined != "" {
		sec := "Requirements mined from the request's identifiers/types (the executor was shown these; " +
			"if the implementation deviates from the recommended construct, check the deviation is justified):\n" + mined
		if plan != "" {
			plan = sec + "\n\n" + plan
		} else {
			plan = sec
		}
	}

	signals, signalSummaries := a.councilSignals(ctx, s, evs, in.fabrication, in.checkLedger, councilActor)
	// Cancellation during verify: unwind rather than persist a misleading convened fact or
	// deliberate on partial evidence.
	if ctx.Err() != nil {
		return false, ""
	}

	// Tell the council when the turn changed nothing (no agent file edits, no signals): a
	// pure read-only / investigation / answer turn has no artifact to verify, so members
	// judge the report on its merits instead of demanding edits that were never going to
	// exist — the consensus rule is unchanged.
	noChanges := strings.TrimSpace(changes) == "" && len(signals) == 0

	// Focused re-round (round-cost reduction 2): from round 2 on, members that
	// already voted done are carried (their vote re-counts under the rule without
	// a re-poll), and only the dissenters re-judge — against the DELTA of actions
	// since their rejection, framed on their own standing concern. This both
	// shrinks the per-round prompt/generation and stops the fresh-nitpick churn a
	// full re-audit invites. Falls back to a full round when everyone dissented.
	pollMembers := members
	actions := turnToolEvidence(evs, councilActionsCap)
	var carried []council.Verdict
	var prior map[string]string
	deltaRound := false
	if ct.rounds >= 2 && len(ct.prevVerdicts) > 0 {
		var repoll []council.Member
		prior = map[string]string{}
		for _, v := range ct.prevVerdicts {
			if v.Decision == council.Done {
				carried = append(carried, v)
				continue
			}
			for _, m := range members {
				if m.Name == v.Member {
					repoll = append(repoll, m)
					break
				}
			}
			if c0 := strings.TrimSpace(v.Feedback); c0 != "" {
				prior[v.Member] = c0
			} else {
				prior[v.Member] = v.Rationale
			}
		}
		if len(carried) > 0 && len(repoll) > 0 {
			deltaRound = true
			pollMembers = repoll
			if from := ct.rejectEvs; from > 0 && from < len(evs) {
				if d := deltaToolEvidence(evs[from:], councilActionsCap); d != "" {
					actions = d
				}
			}
		}
	}

	// Delegated work lives in child sessions the council's own evidence scan cannot see.
	// Fold in this turn's subagent tool evidence so a delegating orchestrator is judged on
	// what its subagents actually did, not just on its synthesis of them.
	if sub := a.subagentTurnEvidence(ctx, sid, evs); sub != "" {
		if actions == "" {
			actions = sub
		} else {
			actions = actions + "\n" + sub
		}
	}

	labels := make([]string, len(pollMembers))
	for i, m := range pollMembers {
		labels[i] = m.Name
	}
	cd, _ := json.Marshal(event.CouncilConvenedData{
		Round: ct.rounds, Members: labels, Rule: string(rule), Signals: signalSummaries,
		Task: task, Plan: plan, Report: in.lastText, Changes: changes, NoChanges: noChanges,
	})
	a.appendFact(ctx, sid, event.TypeCouncilConvened, councilActor, cd)
	// Live panel: announce which members are deliberating this round.
	for _, m := range pollMembers {
		ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: ct.rounds, Member: m.Name, State: "asking"})
		a.publishTransient(sid, event.TypeCouncilDeliberating, councilActor, ld)
	}

	delibStart := time.Now()
	delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
		Round:        ct.rounds,
		Task:         task,
		Plan:         plan,
		Report:       in.lastText,
		Actions:      actions,
		Signals:      signals,
		Changes:      changes,
		NoChanges:    noChanges,
		Members:      pollMembers,
		Rule:         rule,
		Debate:       councilDebateEnabled(),
		Devil:        councilDevilEnabled(),
		Keep:         councilKeepEnabled(),
		DefaultModel: s.Model.Model,
		StepsLeft:    in.stepsLeft,
		DeltaRound:   deltaRound,
		CarriedDone:  carried,
		PriorConcern: prior,
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
	a.emitDebate(sid, councilActor, "terminate", ct.rounds, delib.Debate)
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

	// A FINAL-round rejection lands immediately instead of injecting. Feedback
	// injected here would hand the model one more UNBOUNDED work phase — the round
	// cap above can only fire at the NEXT no-tool-call finish attempt, and a
	// completion-looping model (banner echoes, trivial re-verification commands)
	// never produces one, so the turn burns to the wall clock and the timeout kill
	// takes any running deliverable processes down with it (pypi-server,
	// 2026-07-13). Landing now records the same UNVERIFIED deadlock and leaves the
	// work — files AND processes — standing for verification.
	if ct.rounds >= maxRounds {
		note := fmt.Sprintf("unresolved after %d rounds — finishing; the council never approved this result, treat it as UNVERIFIED; still unmet: %s",
			maxRounds, clipLine(fb, 200))
		emitDecided(council.Done, fb, note, true)
		ct.deadlocked = true
		return false, fmt.Sprintf("the council never approved this result within %d round(s)", maxRounds)
	}

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
	// Advisory keep (MAGI_COUNCIL_KEEP): surface what's already correct ABOVE the fix, so the
	// agent doesn't revert a settled part or re-verify it to exhaustion. It rides only on the
	// injected prompt — ct.feedback stays the raw fb, so no-progress repeat-detection and the
	// recorded decision are unchanged, and the gate is never weakened.
	if k := strings.TrimSpace(delib.Keep); k != "" {
		inject = k + "\n\n" + inject
	}

	emitDecided(council.Continue, fb, "", false)
	// Rejection state for the next round's cost reducers: the action fingerprint
	// (no-progress delta gate), the delta boundary, and this round's verdicts
	// (done votes carry; dissenters get a focused re-round). A delta round's
	// fresh verdicts are merged over the carried ones so a member's latest
	// position always wins.
	ct.rejectToolCalls = countToolCalls(evs)
	ct.rejectEvs = len(evs)
	merged := append([]council.Verdict(nil), delib.Verdicts...)
	seen := map[string]bool{}
	for _, v := range merged {
		seen[v.Member] = true
	}
	for _, v := range carried {
		if !seen[v.Member] {
			merged = append(merged, v)
		}
	}
	ct.prevVerdicts = merged
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: continuationText(inject, task, a.frozenContractClause(a.cachedChecks(sid)))}},
	})
	a.appendFact(ctx, sid, event.TypePromptSubmitted, councilActor, pd)
	return true, ""
}
