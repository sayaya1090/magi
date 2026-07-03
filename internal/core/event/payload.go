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
// Shards make the compaction RE-HYDRATABLE: the lossy summary stays in context,
// but the original messages persist on disk and are indexed here by topic so the
// agent can pull a topic's full detail back on demand (recall_context), instead
// of the detail being lost the way a plain summary loses it.
type CompactionData struct {
	Summary         string         `json:"summary"`
	ReplacesUpToSeq int64          `json:"replacesUpToSeq"`
	TokensBefore    int            `json:"tokensBefore"`
	TokensAfter     int            `json:"tokensAfter"`
	Shards          []ContextShard `json:"shards,omitempty"`
}

// ContextShard indexes one topic within a compacted region: a label/brief the
// agent matches against, plus the IDs of the original messages it covers. The
// messages themselves are not copied — they remain in the event log and are
// rebuilt by ID on recall, so a shard is lossless and cheap to store.
type ContextShard struct {
	Topic      string   `json:"topic"`
	Brief      string   `json:"brief,omitempty"`
	MessageIDs []string `json:"messageIds"`
}

// TurnFinishedData — TypeTurnFinished.
type TurnFinishedData struct {
	Usage Usage `json:"usage"`
}

// TodosChangedData — TypeTodosChanged. The session plan after a change, so the
// progression (seed → steps checked off → completed/cancelled at turn end) is
// persisted and auditable and drives the panel re-render. The full plan is recorded
// each time, so a reader could rebuild the latest state from the log if needed.
type TodosChangedData struct {
	Todos []session.Todo `json:"todos"`
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
	Changes   string `json:"changes,omitempty"`   // this turn's edits, reconstructed from the agent's tools (capped)
	NoChanges bool   `json:"noChanges,omitempty"` // pure read-only turn (no edits/signals)
}

// CouncilVerdictData — TypeCouncilVerdict (one member's vote).
type CouncilVerdictData struct {
	Round      int     `json:"round"`
	Phase      string  `json:"phase,omitempty"` // "" (termination) or "plan" (plan audit) — selects the UI wording
	Member     string  `json:"member"`
	Lens       string  `json:"lens,omitempty"`
	Decision   string  `json:"decision"` // done | continue | abstain
	Confidence float64 `json:"confidence,omitempty"`
	Rationale  string  `json:"rationale,omitempty"`
	Feedback   string  `json:"feedback,omitempty"`
	Severity   string  `json:"severity,omitempty"` // plan audit: critical|warn|info on a revise — what gated the decision
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
	// Criteria is the synthesized completion criteria from a plan-audit approval
	// (plan phase only) — the contract the turn is later judged against.
	Criteria []string `json:"criteria,omitempty"`
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
	// Reason says WHY the prompt fired when the policy forced it (e.g. a bash
	// scan hit: "destructive command detected", "network egress command") —
	// empty for a routine danger-tool confirmation. Shown in the modal so the
	// user decides on the policy's grounds, not just the raw command.
	Reason string `json:"reason,omitempty"`
}

// QuestionRequestedData — TypeQuestionRequested: the agent asks the USER to pick
// among options (the ask_user tool). Index/Total sequence a multi-question call
// (questions are presented one modal at a time, in order).
type QuestionRequestedData struct {
	CallID   string   `json:"callId"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Index    int      `json:"index"` // 1-based position within the call
	Total    int      `json:"total"`
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
