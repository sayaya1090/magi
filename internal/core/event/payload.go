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
	// ParentStep is the index of the parent's plan step this child was spawned to
	// carry out (a delegate/refine write-step). It is the one missing edge that,
	// joined with Parent, lets the live plan tree render a child's own todos indented
	// under this step. Persisted for a future reader; the current resume path only
	// rehydrates top-level sessions, so nothing reads it back yet. nil for children
	// not tied to a plan step (council, scout list, stuck re-decompose). A pointer,
	// since step 0 is valid.
	ParentStep *int `json:"parentStep,omitempty"`
}

// PromptSubmittedData — TypePromptSubmitted (role=user).
type PromptSubmittedData struct {
	MessageID string         `json:"messageId"`
	Parts     []session.Part `json:"parts"`
	// ResurfacedFrom links a re-emitted queued interjection back to the MessageID of
	// the original prompt the user typed. The drain re-runs a queued interjection as
	// its own turn by emitting a fresh prompt (new MessageID); this field lets the
	// display layer pair the query with its answer — dropping the stranded original on
	// replay and pulling the live bubble down to just above the answer. Empty for
	// ordinary prompts/steers.
	ResurfacedFrom string `json:"resurfacedFrom,omitempty"`
}

// PromptAbandonedData — TypePromptAbandoned. Names the cancelled prompt by the
// MessageID of its PromptSubmitted event. seedPromptIdx treats a prompt with a matching
// abandoned marker as resolved (never a turn seed), so a cancelled request does not
// hijack a subsequent unrelated one.
type PromptAbandonedData struct {
	MsgID string `json:"msgId"`
}

// InterjectionDeferredData — TypeInterjectionDeferred. One entry in the deferral
// ledger keyed by the interjection's origin PromptSubmitted MessageID. Resolved:false
// is written when the prompt is queued as an interjection; Resolved:true when it later
// leaves the queue by being absorbed inline or by a route_interjection. A reload treats
// a MessageID with an unresolved entry (and no ResurfacedFrom re-emission) as an
// abandoned interjection to keep masking from the live turn context.
type InterjectionDeferredData struct {
	MessageID string `json:"messageId"`
	Resolved  bool   `json:"resolved,omitempty"`
}

// PartAppendedData — TypePartAppended (a single completed part).
type PartAppendedData struct {
	MessageID string       `json:"messageId"`
	Role      session.Role `json:"role"`
	Part      session.Part `json:"part"`
	// InReplyTo is set on an assistant part that ANSWERS a specific queued/mid-turn
	// user message inline (the idle-park / finish-boundary triage reply), carrying that
	// message's origin MessageID. Unlike a resurfaced interjection (ResurfacedFrom, which
	// re-runs as its own turn), an inline answer produces no fresh prompt, so this is the
	// only link the display layer has to pair the answer with its question and pull the
	// stranded question bubble down just above the answer. Empty for ordinary output.
	InReplyTo string `json:"inReplyTo,omitempty"`
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

// Reduction reports how much this compaction shed: tokens freed (before minus
// after, clamped at 0) and that as a whole-percent share of the pre-compaction
// size (0 when TokensBefore is 0). It backs the human-facing "↯ compacted
// ~X→Y (−Z, −P%)" line in both the headless printer and the TUI, so the size
// difference is stated explicitly rather than left for the reader to subtract.
func (d CompactionData) Reduction() (freed, pct int) {
	freed = d.TokensBefore - d.TokensAfter
	if freed < 0 {
		freed = 0
	}
	if d.TokensBefore > 0 {
		pct = (freed*100 + d.TokensBefore/2) / d.TokensBefore // integer round-to-nearest
	}
	return freed, pct
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
	// Unverified marks a finish the execution-evidence gate could not confirm: a top-level
	// turn changed a deliverable but no independent run passed for the CURRENT version, so the
	// declared outcome — success OR "impossible" — is not backed by execution. The turn still
	// ends (an honest landing, never an infinite block), but is labeled UNVERIFIED rather than
	// laundered into a confident success. Reason carries the short cause. Both empty on a
	// normally-verified finish (the common case).
	Unverified bool   `json:"unverified,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// TodosChangedData — TypeTodosChanged. The session plan after a change, so the
// progression (seed → steps checked off → completed/cancelled at turn end) is
// persisted and auditable and drives the panel re-render. The full plan is recorded
// each time, so a reader could rebuild the latest state from the log if needed.
type TodosChangedData struct {
	Todos []session.Todo `json:"todos"`
}

// ModelChangedData — TypeModelChanged. The session's new active model, so any UI
// caching the model name (header chip, routing editor) re-reads it from one signal
// regardless of which path changed it (plugin set_model, /route edit, reload_config).
type ModelChangedData struct {
	Model string `json:"model"`
}

// UserLabelData — TypeUserLabelChanged. The display name to show for the user in
// the transcript (e.g. an authenticated username injected by an SSO plugin via
// magi.set_user_label). Empty is never broadcast; the UI falls back to "you".
type UserLabelData struct {
	Label string `json:"label"`
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
	// Forced marks a finish the members did NOT themselves approve — a gate-forced
	// landing (round-cap deadlock, cost-cap, council unavailable, no-progress, an
	// unchanged resubmission, or a plan audit proceeding past an unresolved concern).
	// The UI reads this to label the outcome "no consensus" instead of a clean done;
	// it replaces a fragile scan of Note's wording. Absent (false) = genuine decision.
	Forced bool `json:"forced,omitempty"`
	// Criteria is the synthesized completion criteria from a plan-audit approval
	// (plan phase only) — the contract the turn is later judged against.
	Criteria []string `json:"criteria,omitempty"`
}

// StepCheckData — TypeStepCheck (one deterministic deliverable-check execution).
// It carries the pieces separately so the UI can render a clean glyph line
// (✓/✗ + step + deliverable) instead of parsing a formatted note.
type StepCheckData struct {
	Step        string `json:"step,omitempty"`        // the plan step the check belongs to (its label)
	Deliverable string `json:"deliverable,omitempty"` // what the check verifies (human phrase)
	Command     string `json:"command,omitempty"`     // the command that was run
	Code        int    `json:"code"`                  // its exit code
	Pass        bool   `json:"pass"`                  // whether it satisfied the check
}

// CouncilDeliberatingData — TypeCouncilDeliberating (transient, live panel).
// Only "asking" is currently produced (one per member when a round opens); a
// panel infers the "voted" state from the persisted council.verdict facts.
type CouncilDeliberatingData struct {
	Round  int    `json:"round"`
	Member string `json:"member"`
	State  string `json:"state"` // "asking" (emitted) — "voted" inferred from council.verdict
}

// PlanRevisedData — TypePlanRevised (plan-audit convergence). Emitted once per re-plan
// round, right after the planner produces a revised procedure in response to a critical
// council concern. Before/After are the step summaries ("[strategy] title") of the plan
// heading into and coming out of this revision, so a reader can see WHAT changed. Critique
// is the concern the revision was meant to address. Addressed is the judged verdict of
// whether the revision actually engaged that concern — nil when the convergence judge did
// not run (MAGI_PLAN_CONVERGE off), so observability stands alone from the gating.
type PlanRevisedData struct {
	Round     int      `json:"round"`
	Critique  string   `json:"critique,omitempty"`
	Before    []string `json:"before,omitempty"`
	After     []string `json:"after,omitempty"`
	Addressed *bool    `json:"addressed,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

// --- Concern ledger ---

// ConcernRaisedData — TypeConcernRaised. One durable, role-scoped structural signal.
// Key is the STABLE identity used to dedup and to resolve (e.g. "self-check/unverified-premise"):
// a later Raised for the same Key reopens a resolved concern — that is what makes an
// orchestrator reset safe, since a still-true signal re-surfaces on the next fold. The
// Source/Kind/Status/Detail mirror port.Signal so the fold can hand a concern straight to the
// council as evidence. Scope names the origin role/agent (e.g. "self-check", "subagent:scout")
// so a bubbled-up child concern is distinguishable from one raised on this session.
type ConcernRaisedData struct {
	Key    string `json:"key"`
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Scope  string `json:"scope,omitempty"`
}

// ConcernResolvedData — TypeConcernResolved (tombstone by Key). By is "auto" for a
// deterministic recovery (the condition that raised it no longer holds) or "orchestrator"
// for a guarded, judged reset; Reason carries the short cause. A tombstone never deletes the
// raised fact — the ledger fold simply treats the Key as closed until (if ever) re-raised.
type ConcernResolvedData struct {
	Key    string `json:"key"`
	Reason string `json:"reason,omitempty"`
	By     string `json:"by"` // auto | orchestrator
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

// ToolProgressData — TypeToolProgress: a live, best-effort progress note emitted
// by a long-running tool while it blocks (e.g. wait_for polling a readiness
// condition). Transient and droppable — never persisted; the UI shows only the
// latest note and drops it when the tool's result lands.
type ToolProgressData struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
	Text   string `json:"text"`
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
