// Package command defines the inputs that flow into the application (CQRS-lite:
// commands in, events out). Every command carries an Actor for attribution and
// is fully serializable so the same shape works in-process or, later, over a
// remote transport (D5).
package command

import (
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// CreateSession starts a new conversation session.
type CreateSession struct {
	Workdir string           `json:"workdir"`
	Agent   string           `json:"agent"`
	Model   session.ModelRef `json:"model"`
	Actor   event.Actor      `json:"actor"`
}

// SubmitPrompt appends a user prompt and runs the agent loop (async).
type SubmitPrompt struct {
	SessionID session.SessionID `json:"sessionId"`
	Parts     []session.Part    `json:"parts"`
	Actor     event.Actor       `json:"actor"`
}

// Interrupt cancels the in-progress turn for a session.
type Interrupt struct {
	SessionID session.SessionID `json:"sessionId"`
	Actor     event.Actor       `json:"actor"`
}

// RespondPermission answers a pending permission request.
type RespondPermission struct {
	SessionID session.SessionID `json:"sessionId"`
	CallID    string            `json:"callId"`
	Decision  string            `json:"decision"` // allow|deny|always
	Actor     event.Actor       `json:"actor"`
}

// Compact triggers context compaction for a session.
type Compact struct {
	SessionID session.SessionID `json:"sessionId"`
	Actor     event.Actor       `json:"actor"`
}

// ReviewArtifact approves or rejects an emitted artifact (feeds D13).
type ReviewArtifact struct {
	SessionID  session.SessionID `json:"sessionId"`
	ArtifactID string            `json:"artifactId"`
	Decision   string            `json:"decision"` // approve|reject
	Actor      event.Actor       `json:"actor"`
}
