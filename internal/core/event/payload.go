package event

import (
	"fmt"
	"strings"
	"unicode/utf8"

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

// InterjectionAnsweredData — TypeInterjectionAnswered. Names the queued interjection the agent
// says its reply already covered, by the MessageID of the prompt that carried it.
type InterjectionAnsweredData struct {
	MessageID string `json:"messageId"`
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
// SizeNote renders the size change as what it measured, in the one form both surfaces print.
//
// Reduction() clamps a negative saving to zero, which is right for a number called "freed" and
// wrong for the sentence built out of it: a compaction whose summary came out LARGER than what it
// replaced rendered as "(−0, −0%)", which reads as "nothing was freed" when the truth is that the
// context grew. Reachable through the manual /compact — a user who folds a short conversation can
// get a model-written brief longer than the exchange it replaces.
func (d CompactionData) SizeNote() string {
	if d.TokensAfter > d.TokensBefore {
		return fmt.Sprintf("+%d, the summary is LARGER than what it replaced", d.TokensAfter-d.TokensBefore)
	}
	freed, pct := d.Reduction()
	return fmt.Sprintf("−%d, −%d%%", freed, pct)
}

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
// Usage on a turn.finished is what the turn COST: every request made under it — each step's prompt,
// every council poll, every side call, and everything its subagents spent. A parent's numbers
// therefore INCLUDE its children's, so summing these across a session tree would double-count; each
// event is self-describing about its own subtree.
//
// It used to be the agent's own stream alone, with In holding the LAST request's prompt rather than
// a sum — so a twenty-step turn reported one prompt where twenty were paid for, a delegate-heavy
// turn reported almost nothing, and the council's several polls per gate appeared nowhere. The
// context meter still wants that last-prompt number and gets it from its own path (setPromptTokens),
// which is why this one is free to mean the bill.
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

// A "diagnostic" event once recorded a reply the run could not parse and recovered from —
// raw text plus a failure-kind token — so an intermittent parse failure was diagnosable
// after the fact. Its only producer was the shared re-ask helper, removed on 2026-08-02
// when the last of its five callers went; the type, its payload and the TUI line that drew
// it outlived it by an hour. All of it is gone now.
//
// WHAT IS GIVEN UP: a council member whose reply cannot be read twice abstains with the
// rationale "unparseable council reply", and the raw text is discarded. That happened zero
// times in eighty recorded bench sessions, so nothing is bleeding — but if it starts, the
// text is not kept anywhere. Restoring it is not a re-add of this type: the failure happens
// inside the council adapter, which has no event sink, so it needs one threaded in. That is
// a design change, and this note is here so it is made deliberately rather than rediscovered.

// --- Council termination gate (D14) ---

// CouncilConvenedData — TypeCouncilConvened (the gate opens for a round). It also
// carries the EVIDENCE the members were given this round (task/plan/report/diff +
// the no-change flag), so a UI can show what each member judged, not just how they
// voted. Diff is capped to the same budget the council sees.
type CouncilConvenedData struct {
	Round   int      `json:"round"`
	Members []string `json:"members"` // member labels (e.g. Melchior/Balthasar/Casper)
	Rule    string   `json:"rule"`
	// Evidence shown to the members this round (optional, for UI detail views).
	Task   string `json:"task,omitempty"`
	Plan   string `json:"plan,omitempty"`   // acceptance criteria / contract, or the proposed procedure (plan phase)
	Report string `json:"report,omitempty"` // the agent's claim (termination phase)
	// Actions is what the turn's TOOLS produced — the block the members' own prompt calls "real
	// evidence, independent of git", and the one the record used to drop. Everything else here is
	// either the request (task/plan), the agent's own account of it (report), or a reconstruction
	// (changes); this is the only part that is neither asked for nor narrated. Leaving it out made
	// the question "what did the members actually see" unanswerable from the log, and the TUI's
	// evidence view — which mirrors the members' sections — showed every section but this one.
	// Capped to the same budget as the diff so a round of it stays a bounded fact.
	Actions   string `json:"actions,omitempty"`
	Changes   string `json:"changes,omitempty"`   // this turn's edits, reconstructed from the agent's tools (capped)
	NoChanges bool   `json:"noChanges,omitempty"` // pure read-only turn (no edits/signals)
	// Keep records that this round ASKED members for their advisory "keep" (MAGI_COUNCIL_KEEP).
	// Without it an empty CouncilVerdictData.Keep is ambiguous — nobody was asked, or everyone was
	// asked and none answered — and a gate that silently stops asking looks identical to one that
	// asks and gets nothing. That ambiguity is exactly what hid the unwired plan phase for five days.
	Keep bool `json:"keep,omitempty"`
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
	// Cite is the fragment of the record this member said its verdict rests on, or NO-EVIDENCE
	// when it rests on the report's substance rather than on anything observed. It is recorded
	// because it is checkable: magi looks it up in the material the member was shown, so a reader
	// going back through a run can see WHAT each vote was standing on — and an empty one on a
	// "done" is itself worth seeing.
	Cite string `json:"cite,omitempty"`
	// Keep is this member's advisory "what a revision must preserve", recorded per member rather
	// than only in aggregate so a run shows WHICH lens blessed WHICH part. Emitted regardless of
	// Decision: an approving member's keep is precisely what a rewrite forced by another member's
	// objection would otherwise drop.
	Keep string `json:"keep,omitempty"`
}

// CouncilDecidedData — TypeCouncilDecided (the tallied outcome). Feedback is set
// only when the decision is "continue" (it is injected back into the loop).
type CouncilDecidedData struct {
	Round    int               `json:"round"`
	Decision string            `json:"decision"` // done | continue
	Tally    council.Breakdown `json:"tally"`
	Feedback string            `json:"feedback,omitempty"`
	// Note says what the outcome means in words, alongside the tally.
	Note string `json:"note,omitempty"`
	// Debate carries the disagreement-triggered rebuttal round when one ran: the
	// council's decision before and after, and how many members moved. Nil when the
	// independent vote was already unanimous (the common case) or debate is off.
	//
	// The adapter has always computed this "for observability" and nothing observed it:
	// the verdicts that reach the log are the POST-rebuttal ones, emitted with the round
	// hardcoded to 1, so a rebuttal that flipped the outcome was indistinguishable from
	// three members who agreed on the first pass. Whether the round changes anything was
	// therefore unanswerable from a run — which is the only way to know if it earns its
	// three extra calls.
	Debate *council.DebateOutcome `json:"debate,omitempty"`
}

// FeedbackLines renders a rejection's feedback for a human surface: the non-blank lines, each
// truncated, and the whole capped — enough to name every demand holding the turn open without
// letting one verbose member's reasoning bury the transcript. Empty feedback (an approval, or a
// forced finish whose reason is already in the note) renders nothing.
//
// It lives on the payload because BOTH surfaces need it and neither may show less than the other:
// the injected feedback reaches the transcript only as a system note clipped to its first line,
// with the advisory keep-list prepended above it — so the clip is spent on the advisory and the
// objection that actually held the turn open appears nowhere.
func (d CouncilDecidedData) FeedbackLines() []string {
	const maxLines, maxWidth = 12, 200
	var out []string
	for _, ln := range strings.Split(strings.TrimSpace(d.Feedback), "\n") {
		ln = strings.TrimRight(ln, " \t")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if len(out) == maxLines {
			return append(out, fmt.Sprintf("… (feedback continues; %d line(s) shown)", maxLines))
		}
		out = append(out, truncRunes(ln, maxWidth))
	}
	return out
}

// truncRunes cuts s to at most n bytes on a rune boundary (never splitting a multibyte
// character) and marks the cut.
func truncRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
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
