package app

import (
	"fmt"
	"time"
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
