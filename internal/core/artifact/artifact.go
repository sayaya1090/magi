// Package artifact defines the first-class "reviewable output" an agent emits
// (D11). Artifacts are the trust mechanism for parallel/background multi-agent
// work: a child agent reports its result as an artifact that a parent or user
// reviews, rather than watching every step live.
package artifact

import (
	"encoding/json"
	"time"
)

// Kind classifies an artifact. Open-ended (plugins may introduce kinds), but
// these are the well-known ones.
type Kind string

const (
	KindPlan        Kind = "plan"
	KindWalkthrough Kind = "walkthrough"
	KindScreenshot  Kind = "screenshot"
	KindTestReport  Kind = "test-report"
	KindDiff        Kind = "diff"
)

// Status tracks an artifact through review.
type Status string

const (
	StatusDraft    Status = "draft"
	StatusProposed Status = "proposed"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// Artifact is a structured, persisted, reviewable result emitted by an agent.
type Artifact struct {
	ID          string          `json:"id"`
	Kind        Kind            `json:"kind"`
	Title       string          `json:"title"`
	Content     json.RawMessage `json:"content"`
	SourceAgent string          `json:"sourceAgent"`
	Status      Status          `json:"status"`
	Created     time.Time       `json:"created"`
}
