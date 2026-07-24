package app

import (
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// subReport is a subagent's filed final result (the explicit output contract): status leads,
// the answer body carries the result, and the weighted sections close the delegation loop —
// evidence proves "done", deviations surface exceptions, handoff feeds the next step.
type subReport struct {
	summary, status, details      string
	evidence, deviations, handoff string
	substitutions                 string
}

// reportStatusPrefix leads every report frame subReport.result emits: a single
// "STATUS: <WORD>" line the orchestrator and planner parse to tell done from blocked/failed.
const reportStatusPrefix = "STATUS: "

// reportStatusWord extracts the status token of a report frame's leading "STATUS: <WORD>" line
// (upper-cased), or "" when line (trimmed) is not exactly that frame — the single recognizer
// behind refineReportsFailure and stripReportStatus. The "STATUS:" keyword is matched
// case-insensitively; the emitted frame is always upper-case, so this only widens tolerance for
// free-typed model text.
func reportStatusWord(line string) string {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) == 2 && strings.EqualFold(f[0], strings.TrimSpace(reportStatusPrefix)) {
		return strings.ToUpper(f[1])
	}
	return ""
}

// fileReport records a subagent's final report once; later calls in the same
// turn are rejected so a model can't spam it. (output side of the contract)
func (a *App) fileReport(sid session.SessionID, in port.ReportInput) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stateLocked(sid).report != nil {
		return fmt.Errorf("you already filed a report this turn; your turn is ending")
	}
	a.stateLocked(sid).report = &subReport{
		summary: in.Summary, status: in.Status, details: in.Details,
		evidence: in.Evidence, deviations: in.Deviations, handoff: in.Handoff, substitutions: in.Substitutions,
	}
	return nil
}

// takeReport returns and clears any report filed for a session.
func (a *App) takeReport(sid session.SessionID) *subReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.stateLocked(sid).report
	a.stateLocked(sid).report = nil
	return r
}

// addPendingSub registers (or upserts) a declared check substitution awaiting review. It upserts by
// (step, original) so a worker CORRECTING a rejected substitution replaces its prior entry rather than
// stacking a second one. Empty command is ignored.
func (a *App) addPendingSub(sid session.SessionID, sub port.CheckSub) {
	if strings.TrimSpace(sub.Command) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	for i := range st.pendingSubs {
		if strings.TrimSpace(st.pendingSubs[i].Step) == strings.TrimSpace(sub.Step) &&
			strings.TrimSpace(st.pendingSubs[i].Original) == strings.TrimSpace(sub.Original) {
			st.pendingSubs[i] = sub // correction replaces the prior declaration
			return
		}
	}
	st.pendingSubs = append(st.pendingSubs, sub)
}

// pendingSubsOf returns a copy of the substitutions declared this turn (for review).
func (a *App) pendingSubsOf(sid session.SessionID) []port.CheckSub {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok || len(st.pendingSubs) == 0 {
		return nil
	}
	return append([]port.CheckSub(nil), st.pendingSubs...)
}

// clearPendingSubs drops the declared substitutions (after they are approved and applied).
func (a *App) clearPendingSubs(sid session.SessionID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		st.pendingSubs = nil
	}
}

// stashApprovedSubs records a worker's review-approved check substitutions on its (subagent) session
// so the parent's spawn attempt can pick them up (takeApprovedSubs → SpawnResult.CheckSubs) and
// rewrite the matching stored deliverable checks to the working commands.
func (a *App) stashApprovedSubs(sid session.SessionID, subs []port.CheckSub) {
	if len(subs) == 0 {
		return
	}
	a.mu.Lock()
	a.stateLocked(sid).approvedSubs = subs
	a.mu.Unlock()
}

// takeApprovedSubs returns and clears any approved substitutions stashed for a session.
func (a *App) takeApprovedSubs(sid session.SessionID) []port.CheckSub {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return nil
	}
	s := st.approvedSubs
	st.approvedSubs = nil
	return s
}

// result renders the subagent's result around the given answer body, leading with the status so
// the orchestrator can tell done from blocked/failed at a glance, then the weighted sections that
// close the delegation loop — evidence (the proof for a "done" claim), deviations (exceptions the
// orchestrator most needs), handoff (what the next step builds on). Empty sections are omitted so a
// simple report stays short; a section already folded into the answer body is not repeated.
func (r *subReport) result(answer string) string {
	answer = strings.TrimSpace(answer)
	out := reportStatusPrefix + strings.ToUpper(r.status) + "\n" + answer
	section := func(label, body string) {
		if b := strings.TrimSpace(body); b != "" && !strings.Contains(answer, b) {
			out += "\n\n" + label + ": " + b
		}
	}
	section("DETAILS", r.details)
	section("EVIDENCE", r.evidence)
	section("DEVIATIONS", r.deviations)
	section("CHECK-SUBSTITUTION", r.substitutions)
	section("HANDOFF", r.handoff)
	return out
}
