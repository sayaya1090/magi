// Package event defines the event model that is both the persisted log unit
// (event sourcing, D6) and the bus message unit (D5). Every event shares a
// common envelope; the Data field carries a type-specific payload.
//
// Events are split into two classes (see F-EVENT-FACT-TRANSIENT):
//   - Fact events are persisted to the Store and replayed to reconstruct a
//     conversation.
//   - Transient events flow only on the bus (live UX) and are never stored.
package event

import (
	"encoding/json"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// Type names an event variant.
type Type string

// Fact events — persisted, replayable.
const (
	TypeSessionCreated    Type = "session.created"
	TypePromptSubmitted   Type = "prompt.submitted"
	TypePartAppended      Type = "part.appended"
	TypePermissionDecided Type = "permission.decided"
	TypeArtifactEmitted   Type = "artifact.emitted"
	TypeCompaction        Type = "compaction"
	TypeTurnFinished      Type = "turn.finished"
	TypeTodosChanged      Type = "todos.changed"
	TypeError             Type = "error"

	// Council termination gate (D14): the consensus that decides whether the loop
	// finishes or continues. Persisted so the deliberation is replayable/auditable.
	TypeCouncilConvened Type = "council.convened"
	TypeCouncilVerdict  Type = "council.verdict"
	TypeCouncilDecided  Type = "council.decided"

	// Plan-audit convergence (D17): when the council rejects a plan and the planner
	// re-plans, this fact records the round's plan revision (before→after) plus the
	// judged verdict of whether the revision actually addressed the council's concern.
	// Persisted so "was the revision productive" is auditable from the log/trace.
	TypePlanRevised Type = "plan.revised"

	// Concern ledger: a durable, role-scoped structural signal (fabrication,
	// unverified-premise, a subagent's un-recovered concern). Raised deterministically
	// and folded back into the council's evidence, so a signal survives across turns and
	// across the subagent→parent boundary instead of being recomputed-and-discarded each
	// council round. Resolved (tombstone) on deterministic recovery or a guarded
	// orchestrator reset. A later Raised for the same Key reopens it — the property that
	// makes reset safe: a still-true fact re-surfaces on the next fold.
	TypeConcernRaised   Type = "concern.raised"
	TypeConcernResolved Type = "concern.resolved"
)

// Transient events — bus only, not persisted.
const (
	TypePartDelta           Type = "part.delta"
	TypeToolStarted         Type = "tool.started"
	TypeToolProgress        Type = "tool.progress"
	TypePermissionRequested Type = "permission.requested"
	TypeQuestionRequested   Type = "question.requested" // agent asks the USER a multiple-choice question
	TypeAgentSpawned        Type = "agent.spawned"
	TypeAgentStatus         Type = "agent.status"
	TypeContextUsage        Type = "context.usage"
	TypeWorkflowPhase       Type = "workflow.phase"
	TypeCouncilDeliberating Type = "council.deliberating" // a member is being polled (live panel)
	TypeModelChanged        Type = "model.changed"        // session's active model changed at runtime — UI re-reads it
)

// transientTypes is the set of bus-only event types.
var transientTypes = map[Type]bool{
	TypePartDelta:           true,
	TypeToolStarted:         true,
	TypeToolProgress:        true,
	TypePermissionRequested: true,
	TypeQuestionRequested:   true,
	TypeAgentSpawned:        true,
	TypeAgentStatus:         true,
	TypeContextUsage:        true,
	TypeWorkflowPhase:       true,
	TypeCouncilDeliberating: true,
}

// IsTransient reports whether t is a bus-only event type (never persisted).
func (t Type) IsTransient() bool { return transientTypes[t] }

// droppableTypes are HIGH-VOLUME, best-effort live events the bus may drop under
// backpressure (streaming deltas, progress/usage ticks, live council polling).
// Low-volume state transitions (agent.spawned/status, tool.started, …) and all
// facts are NOT droppable — silently losing one desyncs the UI permanently (e.g. a
// subagent pane stuck "running" because its agent.status:"done" was dropped).
var droppableTypes = map[Type]bool{
	TypePartDelta:           true,
	TypeToolProgress:        true,
	TypeContextUsage:        true,
	TypeCouncilDeliberating: true,
}

// Droppable reports whether the bus may discard t under backpressure. Only the
// high-volume streaming/indicator events are droppable; everything else must be
// delivered (see droppableTypes).
func (t Type) Droppable() bool { return droppableTypes[t] }

// IsFact reports whether t is a persisted event type.
func (t Type) IsFact() bool { return !t.IsTransient() }

// ActorKind identifies the category of actor that caused an event.
type ActorKind string

const (
	ActorUser   ActorKind = "user"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
)

// Actor identifies who caused an event (D5 — supports multi-client/origin).
type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   string    `json:"id"` // user id or agent name
}

// Event is the common envelope for everything on the log and the bus.
// Seq is a per-session monotonically increasing sequence number assigned by
// the Store on append; transient (bus-only) events carry Seq == 0.
type Event struct {
	Seq       int64             `json:"seq"`
	SessionID session.SessionID `json:"sessionId"`
	Type      Type              `json:"type"`
	Actor     Actor             `json:"actor"`
	TS        time.Time         `json:"ts"`
	// Stage tags the macro loop phase the event belongs to (D15):
	// plan|execute|council|finalize. It lets the Loop map and rewind/diff group and
	// target events by stage. Empty on older logs.
	Stage string          `json:"stage,omitempty"`
	Data  json.RawMessage `json:"data"`
}
