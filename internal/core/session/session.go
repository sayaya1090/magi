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
	ID      SessionID `json:"id"`
	Workdir string    `json:"workdir"`
	Agent   string    `json:"agent"`            // name of the agent driving this session
	Parent  SessionID `json:"parent,omitempty"` // spawning session (subagents); empty for top-level
	// ParentStep is the index of the parent's plan step this child serves (delegate/
	// refine write-step); nil for children not tied to a plan step. Joined with Parent
	// (PlanChildren) it drives the live plan tree so a child's todos render indented
	// under its step. Runtime-only join key — set on spawn, not rehydrated on resume.
	ParentStep *int              `json:"parentStep,omitempty"`
	Model      ModelRef          `json:"model"`
	Created    time.Time         `json:"created"`
	Meta       map[string]string `json:"meta,omitempty"`
	// Escalatable reports whether this subagent's `ask` can reach an orchestrator
	// that will answer — true only for background-dispatched subagents. Runtime-only
	// (not persisted): a synchronous spawn's parent is blocked awaiting it, so it
	// has no answer loop and `ask` must fail fast instead of blocking on a timeout.
	Escalatable bool `json:"-"`
}

// SessionMeta is a lightweight summary used for listing sessions without
// loading their full event logs.
type SessionMeta struct {
	ID           SessionID `json:"id"`
	Workdir      string    `json:"workdir"`
	Title        string    `json:"title,omitempty"`
	Agent        string    `json:"agent,omitempty"`  // subagent role (child sessions)
	Parent       string    `json:"parent,omitempty"` // spawning session id (child sessions)
	Created      time.Time `json:"created"`
	LastActivity time.Time `json:"lastActivity"`
}

// Message is a single turn authored by one role, composed of ordered parts.
type Message struct {
	ID    string `json:"id"`
	Role  Role   `json:"role"`
	Parts []Part `json:"parts"`
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
