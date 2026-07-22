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
		evidence: in.Evidence, deviations: in.Deviations, handoff: in.Handoff,
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
	section("HANDOFF", r.handoff)
	return out
}
