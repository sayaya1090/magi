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
	Workdir string `json:"workdir"`
	// Parent is the spawning session's id for a CHILD session, empty for a user session. It is
	// what keeps a child out of the resume list (the store hides sessions that have one).
	Parent string `json:"parent,omitempty"`
	// Project is the directory whose project this session's log belongs to, when that is not the
	// Workdir. A child working in its own clone under /tmp still belongs to the PARENT's project —
	// keyed by the clone path its log would land where ChildSessions never scans, and the child
	// would vanish from every view that lists a parent's children. Empty means Workdir.
	Project string           `json:"project,omitempty"`
	Agent   string           `json:"agent"`
	Model   session.ModelRef `json:"model"`
	Actor   event.Actor      `json:"actor"`
}

// SubmitPrompt appends a user prompt and runs the agent loop (async).
type SubmitPrompt struct {
	SessionID session.SessionID `json:"sessionId"`
	Parts     []session.Part    `json:"parts"`
	Actor     event.Actor       `json:"actor"`
	// Refs are files (or line ranges of files) the person attached to this prompt — the IDE's
	// selection, the composer's paperclip. Structured rather than a path:lines convention inside
	// the text, so the words stay the person's words and the excerpt is the CORE's rendering:
	// resolved inside the workspace, sliced, capped, and persisted with the prompt so the
	// transcript shows what the agent was actually shown.
	Refs []FileRef `json:"refs,omitempty"`
}

// FileRef names a file, and optionally the lines of it, attached to a prompt.
type FileRef struct {
	Path string `json:"path"`
	// Lines is "12-40" or "12"; empty means the whole file (up to the cap).
	Lines string `json:"lines,omitempty"`
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

// RespondQuestion answers a pending ask_user question (one option's text, or
// "" when the user dismissed the modal).
type RespondQuestion struct {
	SessionID session.SessionID `json:"sessionId"`
	CallID    string            `json:"callId"`
	Answer    string            `json:"answer"`
	Actor     event.Actor       `json:"actor"`
}

// Compact triggers context compaction for a session.
type Compact struct {
	SessionID session.SessionID `json:"sessionId"`
	Actor     event.Actor       `json:"actor"`
}
