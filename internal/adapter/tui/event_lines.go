package tui

import (
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
)

// The transcript-line builders for facts that carry no per-surface state — free functions rather
// than Model methods so any surface rendering the same fact renders it the same way.

// artifactLine records WHEN a reviewable output was fixed. The content itself has other surfaces,
// but the moment it was fixed had none, so an artifact that changed mid-run left no mark on the
// transcript at all. One line, not a dump.
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
