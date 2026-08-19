package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The council convened by itself at the finish boundary. That placement decided two things it had
// no way to get right: WHEN it was asked — at the one moment the agent had already made up its
// mind — and whether its answer would ever be read, which in a headless run it was not, because
// the advice was injected into a session that was ending in the same tick.
//
// As a tool it is asked when the agent wants it, and the answer returns where every other tool
// result does. What the members see is unchanged: the same record, one lens each.

// councilAdvice runs one deliberation for the council tool and renders the members' readings. The
// question, when the agent supplies one, rides as the task the members weigh — otherwise they weigh
// the turn's own task. Errors come back as errors: an agent told "the council had nothing to add"
// when the backend actually failed would read silence as agreement.
//
// complete marks the call as the agent DECLARING the task finished, which is how a turn ends. The
// council reads the same record as a finish and either accepts — the loop is signalled and the turn
// is over — or hands back what is not done, and the agent keeps working. Ending was a passive event
// before this: the agent stopped calling tools and the turn simply stopped, with nothing asked and
// nothing shown. Now it is an act, and the act is answered.
// councilDoingCall is the call id the council's progress note is filed under.
//
// A council is not a tool call and has no id of its own, but the note is cleared BY id — a
// mechanism built so a finished call cannot blank a running call's line. A constant of its own
// keeps the council's note in that mechanism rather than beside it.
const councilDoingCall = "council"

// councilDoing is the line an outside reader gets while the council sits.
func councilDoing(members, answered int) string {
	if answered >= members {
		return fmt.Sprintf("the council has answered (%d of %d) — reading the verdicts", answered, members)
	}
	return fmt.Sprintf("waiting on the council: %d of %d have answered", answered, members)
}

func (a *App) councilAdvice(ctx context.Context, s session.Session, guardChanges []fileChange, epoch int, question string, complete bool) (string, error) {
	if a.cfg.Council == nil {
		return "", fmt.Errorf("no council is configured for this run")
	}
	sid := s.ID
	councilActor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	members, rule := a.councilParams()

	evs, _ := a.store.Read(ctx, sid, 0)
	evs = a.taskEvents(sid, evs)
	task := lastUserPromptText(evs)
	// The task the loop is actually answering wins when it is known. A redirect interjection
	// re-anchors the goal and masks its own prompt from taskEvents, so lastUserPromptText above
	// falls back to the ABANDONED original — and the council then vetoes completion forever
	// (livelock, observed live). turnTaskNow carries the re-anchored goal.
	if live := strings.TrimSpace(a.turnTaskNow(sid)); live != "" {
		task = live
	}
	if q := strings.TrimSpace(question); q != "" {
		// The agent's question leads, with the turn's task behind it: a member asked only the
		// narrow question cannot tell whether the answer serves the work.
		task = "The agent asks specifically: " + q + "\n\nThe task it is working on: " + task
	}

	// The same evidence the finish gate assembled, in the same order. magi's own record first,
	// because it is the part nobody wrote: which commands ran, how they really ended, and which
	// of them it could not determine.
	actions := turnToolEvidence(evs, councilActionsCap)
	if rec := a.stopRecord(ctx, sid); rec != "" {
		if strings.TrimSpace(actions) == "" {
			actions = rec
		} else {
			actions = rec + "\n\n" + actions
		}
	}
	// A fresh read of the world, taken now — not a replay of the record. This is the part that can
	// contradict both the agent's claim and magi's own log: a file the record says was written and
	// is not there, a file nothing in the record wrote, a server still running, a build that exited
	// nonzero after the last tool call.
	if snap := a.worldDiffFor(sid, s.Workdir, lastUserPromptTS(evs)); snap != "" {
		actions = snap + "\n\n" + actions
	}
	if jobs := a.liveJobsNow(a.jobsStartedBy(ctx, sid)); jobs != "" {
		actions = jobs + "\n\n" + actions
	}
	if gone := missingFromWorld(s.Workdir, a.observe(ctx, sid).changed); len(gone) > 0 {
		actions = "── RECORDED AS WRITTEN, NOT ON DISK NOW ──\n" + strings.Join(clipEach(gone, 8), ", ") +
			"\n\n" + actions
	}
	// First of all: what this council already said and did not accept. It reached the members only
	// through turnToolEvidence before, which keeps the most recent results and drops the rest — so
	// an objection aged out of the council's own evidence after a handful of tool calls, and the
	// round that finally accepted had no way to know it had ever been raised. These are facts magi
	// recorded; handing them back costs nothing and is the difference between judging the work and
	// judging the last few minutes of it.
	if prior := priorCouncilObjections(evs, priorObjectionsCap, councilActionCap); prior != "" {
		actions = prior + "\n\n" + actions
	}
	plan := ""
	if td := a.Todos(sid); len(td) > 0 {
		plan = formatTodos(td)
	}
	changes := truncateForCouncil(buildCouncilChanges(guardChanges), councilDiffCap)
	lastText := lastTurnAssistantText(evs)

	// The fixed harness: magi runs the operator's verification itself and its result leads the
	// evidence. Only on a completion declaration — a question is not a finish to verify. verifyOK is
	// carried past the fan-out so a failing check can refuse the finish whatever the members vote.
	verifyOK := true
	if complete {
		if ev, ok, ran := a.councilVerify(ctx, s.Workdir); ran {
			verifyOK = ok
			actions = ev + "\n\n" + actions
		}
	}

	// A re-declaration with nothing changed is not worth an endless fresh fan-out. When the agent
	// declares complete again with a byte-identical report and the same edits — it did no work,
	// just re-asked — the members are polled again (a median ~87s and three model calls each) to
	// reach the same "no" on the same evidence. Observed live: nine councils on one unchanged
	// sentence. The FIRST repeat still runs, because the members now see their own prior objection
	// fed back and may hold or refine it; from the second identical rejection on, the answer will
	// not move, so it is short-circuited. Only for a completion declaration; a question always runs.
	if complete && identicalRejections(evs, lastText, changes) >= 2 {
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: 1, Decision: string(council.Continue),
			Note: "the agent declared finished again without changing anything since the last councils said no",
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
		// A byte-identical re-declaration is the purest no-progress rejection there is, so it
		// counts toward the cap like any other — without this, an agent pinned here could still
		// loop to the step backstop.
		if landed, msg := a.noteCouncilRejection(sid, epoch, ""); landed {
			return msg + notesTail(a.turnNotesBlock(sid)), nil
		}
		return "The council has already read this exact report twice and did not accept it, and nothing " +
			"has changed since — no new edits, no new result. Do the work the last feedback asked for, or " +
			"state a genuinely different result, then declare completion again. Re-declaring the same thing " +
			"will not change the answer." + notesTail(a.turnNotesBlock(sid)), nil
	}

	labels := make([]string, len(members))
	for i, m := range members {
		labels[i] = m.Name
	}
	cd, _ := json.Marshal(event.CouncilConvenedData{
		Round: 1, Members: labels, Rule: string(rule), Task: task, Plan: plan,
		Report: lastText, Actions: clipEvidenceForRecord(actions, councilDiffCap),
		Changes: changes, NoChanges: strings.TrimSpace(changes) == "",
	})
	a.appendFact(ctx, sid, event.TypeCouncilConvened, councilActor, cd)
	for _, m := range members {
		ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: 1, Member: m.Name, State: "asking"})
		a.publishTransient(sid, event.TypeCouncilDeliberating, councilActor, ld)
	}
	// And where a reader outside this process can be told.
	//
	// The two events above are transient: they reach the terminal drawing this daemon and nobody
	// else. A council takes a median 87 seconds — the longest single thing a turn does — and from
	// an attached window or a console it was 87 seconds of a turn that had simply stopped saying
	// anything. This is the same field a long-running tool writes, which is the field both of
	// those surfaces already read.
	a.noteDoing(sid, councilDoingCall, councilDoing(len(members), 0))
	defer a.clearDoing(sid, councilDoingCall)

	// Atomic because the members are polled CONCURRENTLY and this callback runs on whichever
	// goroutine answers — a plain counter here is a data race, and the race detector runs in CI.
	var answered atomic.Int64
	delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
		Round: 1, Task: task, Plan: plan, Report: lastText, Actions: actions, Changes: changes,
		NoChanges:    strings.TrimSpace(changes) == "",
		Members:      members,
		Rule:         rule,
		DefaultModel: s.Model.Model,
		Debate:       councilDebateEnabled(),
		Keep:         councilKeepEnabled(),
		// Show each member the moment it answers. The members are polled concurrently and the
		// slowest sets the wall clock — a median 87s across the recorded runs, all of it with
		// nothing on screen. The transcript was already built for this: its verdict handler
		// appends into the open round's block and drops that block's render cache precisely so
		// members "streaming in back-to-back" all appear. It was a consumer built for a stream
		// and fed a batch.
		//
		// TRANSIENT, so the record does not change shape: the facts are still written once,
		// below, from the returned Deliberation — which is the post-rebuttal set, and the only
		// one that should be replayable. A live preview that a later round revises is a display
		// concern, and the surfaces that read the log keep counting three verdicts per council.
		OnVerdict: func(v council.Verdict) {
			// Counted as they land, so the line moves: "3 of 3 answered" for a minute and a half
			// is indistinguishable from a line that got stuck.
			a.noteDoing(sid, councilDoingCall, councilDoing(len(members), int(answered.Add(1))))
			vd, _ := json.Marshal(event.CouncilVerdictData{
				Round: 1, Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
				Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback,
				Keep: v.Keep, Cite: v.Cite,
			})
			a.publishTransient(sid, event.TypeCouncilVerdict, councilActor, vd)
		},
	})
	if err != nil {
		return "", fmt.Errorf("the council could not be reached: %w", err)
	}
	a.emitCouncilVerdicts(ctx, sid, councilActor, 1, delib.Verdicts)
	if !complete {
		return renderCouncilAdvice(delib,
			"The council read your work. This is their reading, not a decision — weigh it and judge for yourself."), nil
	}

	// The independent verification has a veto: a "done" vote cannot accept a finish the fixed harness
	// says is not verified. This is the deterministic floor under the members' judgement — the thing
	// an agent cannot talk its way past by disabling its own tests, because magi ran the check.
	accepted := delib.Decision == council.Done && verifyOK
	// The tally rides the FACT even though it is deliberately kept out of what the agent reads:
	// three surfaces render it (the headless transcript, the TUI verdict line, the loop map) and
	// with it left zero they all printed "0 done / 0 continue" under a decision that had three
	// votes behind it — observed live on a run whose three members all voted done.
	// The recorded decision reflects the VETO: members may have voted done, but a finish the fixed
	// harness did not verify is a continue, and the feedback says why so the agent fixes the real
	// failure rather than re-declaring.
	recordedDecision := delib.Decision
	feedback := delib.Feedback
	note := map[bool]string{
		true:  "the agent declared the task finished and the council accepts — the turn ends",
		false: "the agent declared the task finished; the council does not accept it yet",
	}[accepted]
	if delib.Decision == council.Done && !verifyOK {
		recordedDecision = council.Continue
		note = "the members voted to accept, but magi's own verification did not pass — the finish is refused"
		vetoNote := "magi ran the configured verification and it did not pass. The finish is refused on that " +
			"deterministic result, not on the members' vote. Fix what the verification reports above and declare " +
			"completion again; disabling or skipping the check will be seen as a check that ran nothing."
		if strings.TrimSpace(feedback) == "" {
			feedback = vetoNote
		} else {
			feedback = vetoNote + "\n\n" + feedback
		}
	}
	dd, _ := json.Marshal(event.CouncilDecidedData{
		Round: 1, Decision: string(recordedDecision), Tally: delib.Breakdown, Feedback: feedback,
		// The rebuttal round rides the fact for the same reason the tally does: the verdicts
		// recorded here are the ones members held AFTER debating, so without this the round
		// leaves no trace and "did arguing change the outcome" cannot be asked of a run.
		Debate: delib.Debate,
		Note:   note,
	})
	a.appendFact(ctx, sid, event.TypeCouncilDecided, councilActor, dd)
	if accepted {
		a.resetCouncilRejections(sid)
		// The loop reads this at its next step and ends the turn. Signalled rather than returned
		// because the tool result must still reach the transcript: the agent's last word should be
		// its own, not a truncated call.
		a.signalTurnControl(sid, func(tc *turnControl) { tc.finish = true })
		return "The council accepts that the task is finished. Your turn ends here — write your final " +
			"answer for whoever asked, and stop." + notesTail(a.turnNotesBlock(sid)) + "\n\n" +
			renderCouncilAdvice(delib, "What the members said:"), nil
	}
	if delib.Decision == council.Done && !verifyOK {
		return "The finish is REFUSED: magi's own verification did not pass, whatever the members read. " +
			"Fix what the verification reports (shown in the evidence above) and declare completion again." +
			notesTail(a.turnNotesBlock(sid)) + "\n\n" + renderCouncilAdvice(delib, "What the members said:"), nil
	}
	if landed, msg := a.noteCouncilRejection(sid, epoch, feedback); landed {
		return msg + notesTail(a.turnNotesBlock(sid)) + "\n\n" +
			renderCouncilAdvice(delib, "What the members said, for the record:"), nil
	}
	return "The council does NOT accept this as finished yet. Address what follows and declare " +
		"completion again when you believe it is done." + notesTail(a.turnNotesBlock(sid)) + "\n\n" +
		renderCouncilAdvice(delib, "What the members said:"), nil
}

// The rejection cap: how many times one turn's completion declarations may be turned away before
// magi lands the turn UNVERIFIED as it stands.
//
// The consensus gate exists to stop a false "done"; unbounded, it also stopped a TRUE "I could
// not". Measured live, twice in one wave: a task made impossible by the run's own permission mode
// was declared honestly, rejected 0/3, re-declared, rejected — eighteen consecutive rounds over
// forty-six minutes, with the retries the feedback provoked in between, until an external kill.
// The manual promises that an honest failure is a correct outcome; a gate that can only say
// "continue" makes that promise unkeepable whenever the environment withholds the means. The old
// finish-boundary gate had exactly this valve (CouncilMaxRounds → an UNVERIFIED landing, noted);
// the tool-form council lost it.
//
// Two thresholds, because rejection during honest iteration is the gate WORKING: a turn whose
// declarations are separated by real file mutations gets the longer rope, and only a stretch of
// rejections with nothing changing between them — the livelock shape — trips the short one. The
// landing is not an acceptance: the turn ends UNVERIFIED with the reason on the record, the same
// honest shape an undeclared turn gets, and the work stands.
const (
	councilRejectCapStuck = 3 // consecutive rejections with NO mutation between them
	councilRejectCapTotal = 8 // rejections in one turn, mutations or not — the absolute valve
)

// noteCouncilRejection counts one rejected declaration and reports whether the cap landed the
// turn, with the message the agent reads in place of "declare again".
func (a *App) noteCouncilRejection(sid session.SessionID, epoch int, feedback string) (bool, string) {
	if !councilRejectCapEnabled() {
		return false, ""
	}
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.councilRejects++
	if st.councilRejects == 1 || epoch != st.councilRejectEpoch {
		st.councilNoProgress = 1 // first rejection, or real work happened since the last one
	} else {
		st.councilNoProgress++
	}
	st.councilRejectEpoch = epoch
	stuck, total := st.councilNoProgress, st.councilRejects
	a.mu.Unlock()
	if stuck < councilRejectCapStuck && total < councilRejectCapTotal {
		return false, ""
	}
	reason := fmt.Sprintf("the council rejected %d completion declarations (%d in a row with no "+
		"change between them) and the turn was landed UNVERIFIED as it stood", total, stuck)
	a.signalTurnControl(sid, func(tc *turnControl) {
		tc.finish = true
		tc.unverifiedReason = reason
	})
	msg := fmt.Sprintf("The council has now rejected %d declarations this turn", total)
	if stuck >= councilRejectCapStuck {
		msg += fmt.Sprintf(", the last %d with nothing changing in between", stuck)
	}
	msg += ". magi is landing the turn here, recorded as UNVERIFIED: the work stands as it is, and " +
		"the record says the council did not accept it. Write your final answer now — state plainly " +
		"what was done, what was not, and why. An honest account of what could not be done is a " +
		"correct outcome; do not claim completion the record does not back."
	if fb := strings.TrimSpace(feedback); fb != "" {
		msg += "\n\nWhat was still unmet, for your account:\n" + fb
	}
	return true, msg
}

// resetCouncilRejections clears the cap's counters — on an accepted finish here, and at each new
// top-level turn in resetForNewTopLevel.
func (a *App) resetCouncilRejections(sid session.SessionID) {
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.councilRejects, st.councilNoProgress, st.councilRejectEpoch = 0, 0, 0
	a.mu.Unlock()
}

// identicalRejections counts how many of the most recent consecutive councils REJECTED a finish on
// this exact report and these exact changes. It pairs each council.decided with the council.convened
// that preceded it, walks from the end, and stops at the first council that either accepted or judged
// different evidence — so a genuine change (new edits, a new result) resets the count to zero and the
// next declaration runs a fresh fan-out.
func identicalRejections(evs []event.Event, report, changes string) int {
	type outcome struct {
		decision, report, changes string
	}
	var seq []outcome
	var lastConvened *event.CouncilConvenedData
	for _, e := range evs {
		switch e.Type {
		case event.TypeCouncilConvened:
			var d event.CouncilConvenedData
			if json.Unmarshal(e.Data, &d) == nil {
				dd := d
				lastConvened = &dd
			}
		case event.TypeCouncilDecided:
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) == nil && lastConvened != nil {
				seq = append(seq, outcome{d.Decision, lastConvened.Report, lastConvened.Changes})
			}
		}
	}
	n := 0
	for i := len(seq) - 1; i >= 0; i-- {
		if seq[i].decision != "continue" || seq[i].report != report || seq[i].changes != changes {
			break
		}
		n++
	}
	return n
}

// renderCouncilAdvice turns the members' verdicts into what the agent reads: one block per member,
// named by its lens, in its own words, under `lead`. The tally is not rendered — counting votes is
// what the gate did, and a count invites the agent to read a majority as an instruction. Where a
// member had nothing to say beyond agreement, it says so rather than disappearing, so a quiet
// member is not mistaken for one that never answered.
//
// The lead belongs to the CALLER because the three paths mean different things. It used to be one
// fixed line — "this is their reading, not a decision" — which is true when the agent merely asked,
// and flatly contradicts the sentence above it on the other two: the accept path had just said
// "your turn ends here", and the reject path had just said "address what follows". Observed live
// (headless-terminal, 2026-08-01): one reply telling the agent both that its turn was over and
// that this was not a decision to defer to.
func renderCouncilAdvice(d council.Deliberation, lead string) string {
	var b strings.Builder
	b.WriteString(lead + "\n")
	for _, v := range d.Verdicts {
		who := strings.TrimSpace(v.Member)
		if lens := strings.TrimSpace(v.Lens); lens != "" {
			who += " (" + lens + ")"
		}
		say := strings.TrimSpace(v.Feedback)
		if say == "" {
			say = strings.TrimSpace(v.Rationale)
		}
		if say == "" {
			say = "nothing to add."
		}
		b.WriteString("\n── " + who + "\n" + say + "\n")
	}
	if keep := strings.TrimSpace(d.Keep); keep != "" {
		b.WriteString("\n── already correct through some lens (they suggest not redoing these)\n" + keep + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// lastTurnAssistantText is the agent's most recent message — its claim, in its own words, which the
// members read beside the record of what actually ran.
func lastTurnAssistantText(evs []event.Event) string {
	var said []string
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Role != session.RoleAssistant {
			continue
		}
		if d.Part.Kind == session.PartText && strings.TrimSpace(d.Part.Text) != "" {
			said = append(said, d.Part.Text)
		}
	}
	return claimFrom(said)
}

// claimFrom is the agent's report, assembled from the end of what it said.
//
// This kept ONLY the last text, and the last thing an agent says is very often not its report.
// The orchestrator's own reminder tells it to "call the council tool with complete: true", which
// invites a separate short message — and then that message became the whole claim. Reproduced
// exactly: an agent that wrote a report and then said "Requesting council review." had the second
// line handed to the members as its report, and they answered, correctly, that there was none.
// Measured across 54 recorded trials, the last assistant text is under 200 bytes in a THIRD of
// them, so this was not a corner.
//
// So a short tail is treated as what it is — a handoff, not the work — and the substance in front
// of it is carried along. Bounded, because the same trials hold a median 7.5KB and a p90 of 20KB
// of assistant text in total, and the council prompt already budgets everything else it shows.
func claimFrom(said []string) string {
	if len(said) == 0 {
		return ""
	}
	out := said[len(said)-1]
	// Walk back while what we have still reads as a stub, and while there is room.
	for i := len(said) - 2; i >= 0 && len(out) < claimFloor; i-- {
		next := said[i] + "\n\n" + out
		if len(next) > claimCap {
			// Take what fits from this one rather than nothing: its END, which is where a report's
			// conclusion is, and where the text nearest the declaration lives.
			room := claimCap - len(out) - 2
			if room > 200 {
				out = "…" + said[i][len(said[i])-room:] + "\n\n" + out
			}
			break
		}
		out = next
	}
	return out
}

const (
	// claimFloor is the length below which a final message is read as a handoff rather than a
	// report. A declaration ("Requesting council review.", "Done — declaring completion") is a
	// line or two; a report that fits in 400 bytes is a report the council can judge on its own.
	claimFloor = 400
	// claimCap bounds the assembled claim. Sized against councilDiffCap, which is what the same
	// prompt allows the whole action record: the claim must not be able to crowd out magi's own
	// evidence, which is the part the agent did not write.
	claimCap = 6000
)
