package event

import (
	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/council"
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

// --- Council termination gate (D14) ---

// CouncilConvenedData — TypeCouncilConvened (the gate opens for a round). It also
// carries the EVIDENCE the members were given this round (task/plan/report/diff +
// the no-change flag), so a UI can show what each member judged, not just how they
// voted. Diff is capped to the same budget the council sees.
type CouncilConvenedData struct {
	Round int `json:"round"`
	// Phase is "" (turn-termination gate) or "plan" (pre-flight plan audit).
	Phase   string   `json:"phase,omitempty"`
	Members []string `json:"members"` // member labels (e.g. Melchior/Balthasar/Casper)
	Rule    string   `json:"rule"`
	Signals []string `json:"signals,omitempty"` // human summaries of evidence fed in, e.g. "verify/test: fail"
	// Evidence shown to the members this round (optional, for UI detail views).
	Task      string `json:"task,omitempty"`
	Plan      string `json:"plan,omitempty"`      // acceptance criteria / contract, or the proposed procedure (plan phase)
	Report    string `json:"report,omitempty"`    // the agent's claim (termination phase)
	Diff      string `json:"diff,omitempty"`      // working diff (capped)
	NoChanges bool   `json:"noChanges,omitempty"` // pure read-only turn (no diff/signals)
}

// CouncilVerdictData — TypeCouncilVerdict (one member's vote).
type CouncilVerdictData struct {
	Round      int     `json:"round"`
	Member     string  `json:"member"`
	Lens       string  `json:"lens,omitempty"`
	Decision   string  `json:"decision"` // done | continue | abstain
	Confidence float64 `json:"confidence,omitempty"`
	Rationale  string  `json:"rationale,omitempty"`
	Feedback   string  `json:"feedback,omitempty"`
}

// CouncilDecidedData — TypeCouncilDecided (the tallied outcome). Feedback is set
// only when the decision is "continue" (it is injected back into the loop).
type CouncilDecidedData struct {
	Round    int               `json:"round"`
	Phase    string            `json:"phase,omitempty"` // "" (termination) or "plan" (plan audit)
	Decision string            `json:"decision"`        // done | continue
	Tally    council.Breakdown `json:"tally"`
	Feedback string            `json:"feedback,omitempty"`
	// Note explains a gate-forced finish (e.g. round cap reached or no progress),
	// when the members did not themselves vote done. Empty for a normal decision.
	Note string `json:"note,omitempty"`
}

// CouncilDeliberatingData — TypeCouncilDeliberating (transient, live panel).
// Only "asking" is currently produced (one per member when a round opens); a
// panel infers the "voted" state from the persisted council.verdict facts.
type CouncilDeliberatingData struct {
	Round  int    `json:"round"`
	Member string `json:"member"`
	State  string `json:"state"` // "asking" (emitted) — "voted" inferred from council.verdict
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

// ContextUsageData — TypeContextUsage (live context meter). Tokens is the current
// input/context size (the "↑" readout); OutTokens is the turn's cumulative output
// so far (the "↓" readout), letting the UI show live token usage.
type ContextUsageData struct {
	Tokens    int     `json:"tokens"`
	Window    int     `json:"window"`
	Percent   float64 `json:"percent"`
	OutTokens int     `json:"outTokens,omitempty"`
}

// WorkflowPhaseData — TypeWorkflowPhase (deterministic pipeline progress).
type WorkflowPhaseData struct {
	Phase  string `json:"phase"`            // localize | implement | verify | review | summarize
	Status string `json:"status"`           // start | done | pass | fail | retry
	Detail string `json:"detail,omitempty"` // e.g. "exit 1", "attempt 2/3"
}
