package app

import (
	"fmt"
	"time"
)

// councilDiffCap / councilSignalCap bound the diff and verify output embedded in
// each member's prompt so they don't multiply token cost by the member count.
const (
	councilDiffCap    = 6000
	councilActionsCap = 8 // most recent turn outputs (model text + tool results) shown to the council
	// priorObjectionsCap bounds how many of the council's own earlier objections are handed back
	// (priorCouncilObjections). It is a small number on purpose: the point is that a concern does
	// not vanish after a few tool calls, not that every round accumulates the whole argument.
	priorObjectionsCap = 6
	councilActionCap   = 4000 // per-item byte cap — 400 was far too tight: it clipped a file/output mid-content
	// (e.g. a `cat script.py` whose bug was past byte 400), so the council could see a wrong
	// RESULT but not the CAUSE, and its feedback stayed symptom-level round after round. Kept in
	// the same ballpark as councilDiffCap so a whole small file/output is visible, not a fragment.
)

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
