// Package artifact defines the first-class "reviewable output" an agent emits
// (D11). Artifacts are the trust mechanism for parallel/background multi-agent
// work: a child agent reports its result as an artifact that a parent or user
// reviews, rather than watching every step live.
package artifact

import (
	"encoding/json"
	"time"
)

// Kind classifies an artifact, and Status tracks it through review. Both are open strings:
// the well-known values were spelled out as constants until the emit path went, which left an
// enumeration nothing could produce. The types stay because the record does — a session logged
// before that removal still decodes through them.
type Kind string

// Status tracks an artifact through review.
type Status string

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
