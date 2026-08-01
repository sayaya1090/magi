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
