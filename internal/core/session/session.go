// Package session defines the core domain types for conversations: sessions,
// messages, and the streaming/persisted parts that compose them.
//
// This package is pure domain — it imports nothing outside the standard library
// and other core packages. Adapters depend on it, never the reverse.
package session

import (
	"encoding/json"
	"time"
)

// SessionID uniquely identifies a conversation session.
type SessionID string

// Role identifies who authored a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
)

// ModelRef points at a concrete model behind a provider.
type ModelRef struct {
	Provider string `json:"provider"` // e.g. "openai" (OpenAI-compatible base)
	Model    string `json:"model"`    // e.g. "qwen2.5-coder"
}

// Session is the top-level unit of organization for a conversation.
type Session struct {
	ID      SessionID         `json:"id"`
	Workdir string            `json:"workdir"`
	Agent   string            `json:"agent"` // name of the agent driving this session
	Model   ModelRef          `json:"model"`
	Created time.Time         `json:"created"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// SessionMeta is a lightweight summary used for listing sessions without
// loading their full event logs.
type SessionMeta struct {
	ID      SessionID `json:"id"`
	Workdir string    `json:"workdir"`
	Title   string    `json:"title,omitempty"`
	Agent   string    `json:"agent,omitempty"` // subagent role (child sessions)
	// Model is what the session was OPENED with, off its first event. Carried because a list of
	// past work is a list of work done by something, and which engine did it is the one fact about
	// a finished session that is neither in its title nor derivable later — a companion's model can
	// be changed mid-life by /route, so the one it is on now says nothing about last Tuesday.
	//
	// Free: scanSessions already unmarshals the created event to find the parent, and this was
	// sitting in the same struct being thrown away.
	Model string `json:"model,omitempty"`
	// Labels is what the agent said this work was about, as of the last time it said so. Free to
	// carry: the scan that builds this already reads every event to find the title.
	Labels []string `json:"labels,omitempty"`
	Parent string   `json:"parent,omitempty"` // spawning session id (child sessions)
	// Origin is the actor id that opened the session — "cli", "tui", "cron:<name>". It answers a
	// question a list of past work cannot otherwise answer: whether a person asked for this or
	// something did it unattended. The scheduled-work editors read it to show a job's last run,
	// which is why no separate run ledger exists — a second record of when something happened is a
	// second record that can disagree with the first.
	//
	// Free to carry: the scan that builds this already unmarshals the created event to find the
	// parent, and the actor is on the same envelope.
	Origin       string    `json:"origin,omitempty"`
	Created      time.Time `json:"created"`
	LastActivity time.Time `json:"lastActivity"`
}

// Message is a single turn authored by one role, composed of ordered parts.
type Message struct {
	ID    string `json:"id"`
	Role  Role   `json:"role"`
	Parts []Part `json:"parts"`
	// At is when the message began — the timestamp of the event that opened it.
	//
	// A message is not persisted; it is rebuilt from the log, and the log has always carried the
	// time on every envelope. Dropping it in the rebuild meant the time existed only in whatever
	// UI happened to be watching live: the terminal stamped its blocks from the events as they
	// arrived, so the same conversation reopened tomorrow — or read from the console, which only
	// ever reads rebuilt messages — had no times on it at all.
	//
	// Zero for anything assembled rather than replayed (a prompt on its way to the model, a
	// compaction summary), and a zero time is shown as nothing rather than as 1970.
	At time.Time `json:"at,omitzero"`
}

// PartKind discriminates the variant of a Part (tagged union).
type PartKind string

const (
	PartText       PartKind = "text"
	PartReasoning  PartKind = "reasoning"
	PartToolCall   PartKind = "tool-call"
	PartToolResult PartKind = "tool-result"
	PartImage      PartKind = "image"
	PartError      PartKind = "error"
)

// Part is the smallest stream/persist unit of a message. Exactly one of the
// kind-specific fields is populated, selected by Kind.
type Part struct {
	ID   string   `json:"id"`
	Kind PartKind `json:"kind"`

	Text       string      `json:"text,omitempty"`       // PartText | PartReasoning
	ToolCall   *ToolCall   `json:"toolCall,omitempty"`   // PartToolCall
	ToolResult *ToolResult `json:"toolResult,omitempty"` // PartToolResult
	Image      *ImageRef   `json:"image,omitempty"`      // PartImage
	Err        string      `json:"error,omitempty"`      // PartError
}

// ToolCall is a model's request to invoke a tool with JSON arguments.
type ToolCall struct {
	CallID string          `json:"callId"`
	Name   string          `json:"name"`
	Args   json.RawMessage `json:"args"`
}

// ToolResult is the outcome of executing a ToolCall.
type ToolResult struct {
	CallID  string          `json:"callId"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"isError,omitempty"`
}

// ImageRef references image data stored outside the event log (file path or
// blob hash); the log carries only the reference to keep it small.
type ImageRef struct {
	Path string `json:"path"`
	MIME string `json:"mime"`
}

// Todo is one item in an agent's plan (TodoWrite). Status is
// pending|in_progress|completed.
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}
