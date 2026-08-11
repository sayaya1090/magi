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
	TypeCompaction        Type = "compaction"
	TypeTurnFinished      Type = "turn.finished"
	TypeTodosChanged      Type = "todos.changed"
	// TypeLabelsChanged — what the agent says this piece of work is ABOUT, in its own words.
	//
	// Recorded rather than derived, and that is not a hole in "derive, never record": the rule
	// exists so state cannot go stale, and a label is not state — it is a judgement made while the
	// work was happening, like a todo or a remembered fact. Nothing can derive it afterwards,
	// because "this was the billing refactor" is not visible in any count of turns or errors.
	TypeLabelsChanged Type = "labels.changed"
	TypeError         Type = "error"

	// Council termination gate (D14): the consensus that decides whether the loop
	// finishes or continues. Persisted so the deliberation is replayable/auditable.
	TypeCouncilConvened Type = "council.convened"
	TypeCouncilVerdict  Type = "council.verdict"
	TypeCouncilDecided  Type = "council.decided"

	// Interjection deferral ledger (F5): a durable record that a user prompt was queued
	// as a mid-turn interjection (Resolved:false at enqueue) and, later, that it left the
	// queue by being absorbed inline/by a route (Resolved:true). Drain-to-own-turn is
	// already recorded by the resurfaced prompt's ResurfacedFrom link, so it needs no
	// mark here. The in-memory queue that masks a live interjection does not survive a
	// process kill; this ledger lets a reload reconstruct which queued interjections were
	// never resolved (deferred-but-abandoned) so they stay masked from turn context and
	// are not silently mixed into the next request, instead of leaking as pending prompts.
	TypeInterjectionDeferred Type = "interjection.deferred"
	// TypeInterjectionAnswered — the agent stated, via route_interjection action "answered", that
	// its reply already covers a queued interjection. It is a CLAIM, not a resolution: the finish
	// boundary still checks whether the turn said anything after it. The display layer needs it
	// anyway, because until this signal exists a bubble the model has already answered keeps its
	// pending marker and its pinned position with nothing able to tell it otherwise.
	TypeInterjectionAnswered Type = "interjection.answered"

	// TypePromptAbandoned records that a user prompt's turn was cancelled (Interrupt)
	// before it produced any answer, so the prompt must not seed a LATER, unrelated
	// request. Without it seedPromptIdx keeps picking the cancelled prompt as the first
	// "unanswered" one and anchors the next turn onto it (treating the genuinely new
	// prompt as a mere interjection). Persisted so the abandonment survives a reload and
	// is visible to seedPromptIdx, which reads the log; ignored by reconstruct, so the
	// cancelled prompt's text stays in context (a follow-up that augments it still works).
	TypePromptAbandoned Type = "prompt.abandoned"
)

// Transient events — bus only, not persisted.
const (
	TypePartDelta           Type = "part.delta"
	TypeToolProgress        Type = "tool.progress"
	TypePermissionRequested Type = "permission.requested"
	TypeQuestionRequested   Type = "question.requested" // agent asks the USER a multiple-choice question
	TypeContextUsage        Type = "context.usage"
	TypeWorkflowPhase       Type = "workflow.phase"
	TypeCouncilDeliberating Type = "council.deliberating" // a member is being polled (live panel)
	TypeModelChanged        Type = "model.changed"        // session's active model changed at runtime — UI re-reads it
	TypeUserLabelChanged    Type = "user.label.changed"   // user display label changed (plugin set_user_label) — UI re-reads it
	// TypeQuestionAnswered — somebody answered an ask_user prompt. Announced because a prompt can
	// be answered from a DIFFERENT process than the one showing it: a browser and a terminal on one
	// daemon is the arrangement the socket exists for, and the second screen was left holding a
	// question that had already been decided. A permission has permission.decided for this; a
	// question had nothing, because its answer goes straight down a channel to the waiting tool.
	TypeQuestionAnswered Type = "question.answered"
)

// transientTypes is the set of bus-only event types.
var transientTypes = map[Type]bool{
	TypePartDelta:           true,
	TypeToolProgress:        true,
	TypePermissionRequested: true,
	TypeQuestionRequested:   true,
	TypeContextUsage:        true,
	TypeWorkflowPhase:       true,
	TypeCouncilDeliberating: true,
	TypeQuestionAnswered:    true,
}

// IsTransient reports whether t is a bus-only event type (never persisted).
func (t Type) IsTransient() bool { return transientTypes[t] }

// droppableTypes are HIGH-VOLUME, best-effort live events the bus may drop under
// backpressure (streaming deltas, progress/usage ticks, live council polling).
// Low-volume state transitions (permission/question requests, …) and all facts are NOT
// droppable — silently losing one desyncs the UI permanently (e.g. a tool row stuck
// "running" because the result that closed it was dropped).
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
	Data      json.RawMessage   `json:"data"`
}
