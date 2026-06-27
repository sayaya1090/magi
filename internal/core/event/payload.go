package event

import (
	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Payload structs are the typed shapes carried in Event.Data for each Type.
// They are marshaled into Event.Data (json.RawMessage) when publishing and
// unmarshaled when consuming.

// SessionCreatedData — TypeSessionCreated.
type SessionCreatedData struct {
	Workdir string           `json:"workdir"`
	Agent   string           `json:"agent"`
	Model   session.ModelRef `json:"model"`
	// Parent is set for subagent (child) sessions to the spawning session's id;
	// empty for top-level user sessions. Lets the resume list hide subagents.
	Parent string `json:"parent,omitempty"`
}

// PromptSubmittedData — TypePromptSubmitted (role=user).
type PromptSubmittedData struct {
	MessageID string         `json:"messageId"`
	Parts     []session.Part `json:"parts"`
}

// PartAppendedData — TypePartAppended (a single completed part).
type PartAppendedData struct {
	MessageID string       `json:"messageId"`
	Role      session.Role `json:"role"`
	Part      session.Part `json:"part"`
}

// PermissionDecidedData — TypePermissionDecided (audit trail).
type PermissionDecidedData struct {
	CallID   string `json:"callId"`
	Decision string `json:"decision"` // allow|deny|always
}

// ArtifactEmittedData — TypeArtifactEmitted.
type ArtifactEmittedData struct {
	Artifact artifact.Artifact `json:"artifact"`
}

// CompactionData — TypeCompaction (summary snapshot replacing prior events).
type CompactionData struct {
	Summary         string `json:"summary"`
	ReplacesUpToSeq int64  `json:"replacesUpToSeq"`
	TokensBefore    int    `json:"tokensBefore"`
	TokensAfter     int    `json:"tokensAfter"`
}

// TurnFinishedData — TypeTurnFinished.
type TurnFinishedData struct {
	Usage Usage `json:"usage"`
}

// Usage captures token/cost accounting for a turn.
type Usage struct {
	In   int     `json:"in"`
	Out  int     `json:"out"`
	Cost float64 `json:"cost,omitempty"`
}

// ErrorData — TypeError.
type ErrorData struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// --- Transient payloads (bus only) ---

// PartDeltaData — TypePartDelta (streaming text chunk).
type PartDeltaData struct {
	MessageID string           `json:"messageId"`
	PartID    string           `json:"partId"`
	Kind      session.PartKind `json:"kind"`
	Text      string           `json:"text"`
}

// ToolStartedData — TypeToolStarted.
type ToolStartedData struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
}

// PermissionRequestedData — TypePermissionRequested (UI prompt).
type PermissionRequestedData struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
	Args   []byte `json:"args"`
}

// AgentStatusData — TypeAgentSpawned / TypeAgentStatus (multi-agent live view).
type AgentStatusData struct {
	AgentID string `json:"agentId"`
	Parent  string `json:"parent,omitempty"`
	Role    string `json:"role,omitempty"`
	State   string `json:"state"`
}

// ContextUsageData — TypeContextUsage (live context meter).
type ContextUsageData struct {
	Tokens  int     `json:"tokens"`
	Window  int     `json:"window"`
	Percent float64 `json:"percent"`
}

// WorkflowPhaseData — TypeWorkflowPhase (deterministic pipeline progress).
type WorkflowPhaseData struct {
	Phase  string `json:"phase"`            // localize | implement | verify | review | summarize
	Status string `json:"status"`           // start | done | pass | fail | retry
	Detail string `json:"detail,omitempty"` // e.g. "exit 1", "attempt 2/3"
}
