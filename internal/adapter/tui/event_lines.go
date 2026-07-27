package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
)

// The transcript-line builders for facts that carry no per-surface state. They are free
// functions rather than Model methods because BOTH surfaces render the same facts — the main
// transcript and a subagent's own pane (a worker runs its own planner, council and checks, and
// those events land on the worker's session). A fact rendered in only one of the two is a fact
// the user cannot see whenever the work happened to be delegated.

// stepCheckLine renders one deterministic deliverable-check result as a clean, readable line —
// a green ✓ or red ✗, the step label, and what it verifies — instead of the council-round
// wrapper these facts used to be shoved through ("round 0: finished (no consensus) — 0 done /
// 0 continue (check [1] …)"). A single executed check has no round or tally; this shows just
// the result.
func stepCheckLine(d event.StepCheckData) string {
	what := strings.TrimSpace(d.Deliverable)
	if what == "" {
		// A typed check has no command; its source+assertion is what it ran.
		if what = strings.TrimSpace(d.Command); what == "" {
			what = strings.TrimSpace(strings.TrimPrefix(d.Source+": "+d.Assert, ": "))
		}
	}
	if step := strings.TrimSpace(d.Step); step != "" {
		what = "[" + step + "] " + what
	}
	glyph, tail := "✓", ""
	color := colSuccess
	if !d.Pass {
		glyph, color = "✗", colError
		tail = fmt.Sprintf(" — exit %d", d.Code)
	}
	return lipgloss.NewStyle().Foreground(color).Render(glyph+" check ") + what + tail
}

// planRevisedLine renders one plan-audit re-plan round: the critique that forced it, the
// before→after step diff (− removed, + added — colored like a code diff, since that is what it
// is), and, when the convergence judge ran, whether the revision actually engaged the critique.
// A revision that changed nothing renders as such rather than as an empty block: "no steps
// changed" is the diagnosis, not the absence of one.
func planRevisedLine(d event.PlanRevisedData) string {
	line := fmt.Sprintf("⟳ plan revised (round %d)", d.Round)
	if c := strings.TrimSpace(d.Critique); c != "" {
		line += ": " + clipLine(c, 200)
	}
	added, removed := d.Diff()
	del := lipgloss.NewStyle().Foreground(colError)
	ins := lipgloss.NewStyle().Foreground(colSuccess)
	for _, s := range removed {
		line += "\n    " + del.Render("− "+clipLine(s, 120))
	}
	for _, s := range added {
		line += "\n    " + ins.Render("+ "+clipLine(s, 120))
	}
	if len(added) == 0 && len(removed) == 0 {
		line += "\n    " + styleFooter.Render("(no steps changed)")
	}
	if d.Addressed != nil {
		mark, st := "no", del
		if *d.Addressed {
			mark, st = "yes", ins
		}
		verdict := "→ addressed=" + mark
		if r := strings.TrimSpace(d.Reason); r != "" {
			verdict += ": " + clipLine(r, 200)
		}
		line += "\n    " + st.Render(verdict)
	}
	return line
}

// artifactLine records WHEN a reviewable output was fixed (acceptance criteria, deliverable
// checks, a check audit). The content itself has other surfaces — the panel's per-step
// deliverables, the council detail's ledger — but the moment it was fixed had none, so a
// contract that changed mid-run left no mark on the transcript at all. One line, not a dump.
func artifactLine(d event.ArtifactEmittedData) string {
	title := strings.TrimSpace(d.Artifact.Title)
	if title == "" {
		title = strings.TrimSpace(string(d.Artifact.Kind))
	}
	if title == "" {
		return ""
	}
	return "◇ " + clipLine(title, 120)
}

// concernRaisedLine renders a durable structural signal the moment it opens — a self-check that
// found an unverified premise, or a child agent's concern bubbled onto the parent. Until now the
// concern ledger had exactly one surface: the council detail modal, which requires a council round
// to have produced a verdict block AND the user to click a member. A concern raised on a turn that
// never convenes a council was therefore recorded, injected into later prompts, and never shown to
// anyone. Concerns are deduped by Key at the producer, so this is one line per open, not per round.
func concernRaisedLine(d event.ConcernRaisedData) string {
	tag := strings.Trim(strings.TrimSpace(d.Source)+"/"+strings.TrimSpace(d.Kind), "/")
	if tag == "" {
		if tag = strings.TrimSpace(d.Key); tag == "" {
			return ""
		}
	}
	line := "⚑ concern [" + tag + "]"
	if st := strings.TrimSpace(d.Status); st != "" {
		line += " " + st
	}
	if dt := strings.TrimSpace(d.Detail); dt != "" {
		line += " — " + clipLine(dt, 240)
	}
	return lipgloss.NewStyle().Foreground(colWarn).Render(line)
}

// concernResolvedLine is the other half: a concern that closes without a line reads, to anyone
// watching, as one that is still open — the ledger's own dedup means it will never be announced
// again either.
func concernResolvedLine(d event.ConcernResolvedData) string {
	key := strings.TrimSpace(d.Key)
	if key == "" {
		return ""
	}
	line := "⚐ concern resolved [" + key + "]"
	if by := strings.TrimSpace(d.By); by != "" {
		line += " by " + by
	}
	if r := strings.TrimSpace(d.Reason); r != "" {
		line += " — " + clipLine(r, 240)
	}
	return lipgloss.NewStyle().Foreground(colSuccess).Render(line)
}

// diagnosticLine renders a reply the run could not use and recovered from (a planner/council
// pass whose JSON was malformed). It is persisted at full fidelity precisely because it is
// otherwise lost — but nothing rendered it, so on screen a pass that was thrown away looked
// exactly like one that never ran, and a run that silently retried its planner three times
// looked like a run that planned once. The raw reply stays in the event log; this is the
// pointer to it.
func diagnosticLine(d event.DiagnosticData) string {
	src := strings.TrimSpace(d.Source)
	if src == "" {
		return ""
	}
	line := "⚠ " + src + ": unusable reply"
	if k := strings.TrimSpace(d.Kind); k != "" {
		line += " (" + k + ")"
	}
	return line
}
