package event

import (
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"

	"github.com/sayaya1090/magi/internal/core/text"
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
	// Project, when set, names the directory whose project this log belongs to instead of
	// Workdir — a child in its own temp clone files under its parent's project so the views
	// that list children can find it. The store's path routing reads this.
	Project string `json:"project,omitempty"`
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

// QuestionAnsweredData — TypeQuestionAnswered. Names the prompt that was answered, so a screen
// showing it knows to put it away. The answer itself rides along for the surfaces that want to say
// what was chosen rather than only that something was.
type QuestionAnsweredData struct {
	CallID string `json:"callId"`
	Answer string `json:"answer,omitempty"`
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

// ResultElidedData — TypeResultElided. One tool result, replaced in the MODEL's view by a short
// stub because the window was closing and this result was the cheapest thing to give up: bulky,
// already digested (the assistant narrated its conclusions right after it), and re-derivable by
// re-reading the file or re-running the command. The original is never touched — it stays in the
// log, the person's view shows it, and only reconstruct's model view substitutes the stub.
//
// Eliding near the TAIL is the point: a prompt cache matches from the first byte, so replacing an
// old message re-bills everything after it, while replacing a recent one re-bills almost nothing.
// The summarising fold does the opposite and stays the fallback for what this cannot reclaim.
type ResultElidedData struct {
	CallID string `json:"callId"`
	Bytes  int    `json:"bytes"`
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

// SessionMovedData — TypeSessionMoved. Where the companion went, so a reader can follow it.
type SessionMovedData struct {
	To session.SessionID `json:"to"`
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
	// Prompt is what the request this turn ended on was MADE OF. Usage says what the turn cost;
	// this says what the cost was spent on, and only the process that assembled the request can
	// answer it — the system prompt and the tool catalog are built per session and never written
	// to the log, so a reader replaying events can measure the conversation and nothing else.
	// Recorded once per turn rather than per step because it is stable within a turn by
	// construction (the prompt is frozen at turn start so the backend's prefix cache holds).
	Prompt *PromptShape `json:"prompt,omitempty"`
}

// PromptShape is the estimated make-up of one request, in tokens.
//
// Estimates throughout — chars/4, the same arithmetic the compactor sizes with — because no
// backend reports a breakdown. They are honest as proportions; they will not sum to the provider's
// measured prompt count, and a reader must not present them as if they did.
//
// Why it is worth recording at all: on the default roster the tool catalog alone measured 6-7k
// tokens, which is larger than most conversations ever get. A screen showing only a total invites
// somebody watching a full window to go trim the conversation, which is usually the small half.
type PromptShape struct {
	// Window is the model's context window at the moment this request was assembled, or 0 when
	// this process did not know it.
	//
	// It rides here because a reader cannot work it out. The window comes from a model registry
	// and a backend probe, and a console loads neither: it builds its own App over the log with an
	// empty registry and no prober, so it answered 0 for every companion and drew no gauge at all
	// — measured against a daemon that had probed the same model and knew 262,144. "The window is
	// unknown" and "this backend has no limit" are the same 0 to that reader, and the screen has a
	// rule about the difference it could not act on.
	Window  int `json:"window,omitempty"`
	System  int `json:"system,omitempty"`  // identity, workdir, memory, skills
	Tools   int `json:"tools,omitempty"`   // the tool catalog: names, descriptions, schemas
	Talk    int `json:"talk,omitempty"`    // what the person and the companion said
	Calls   int `json:"calls,omitempty"`   // tool calls: names and arguments
	Results int `json:"results,omitempty"` // what the tools answered
}

// TodosChangedData — TypeTodosChanged. The session plan after a change, so the
// progression (seed → steps checked off → completed/cancelled at turn end) is
// persisted and auditable and drives the panel re-render. The full plan is recorded
// each time, so a reader could rebuild the latest state from the log if needed.
type TodosChangedData struct {
	Todos []session.Todo `json:"todos"`
}

// LabelsChangedData — TypeLabelsChanged. The whole set each time, not a delta: a reader that has to
// replay every add and remove to know the current labels is a reader that gets it wrong the first
// time an event is missed, and the set is a handful of short strings.
type LabelsChangedData struct {
	Labels []string `json:"labels"`
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
	// Cached is the part of In the backend served from its own prompt cache, and CacheReported says
	// whether it mentioned a cache at all. Two fields for one number because zero and silence are
	// different facts: a backend that reports 0 is saying the cache missed, and one that reports
	// nothing is saying nothing — shown as 0% they would both read as a cache that never works.
	Cached        int  `json:"cached,omitempty"`
	CacheReported bool `json:"cacheReported,omitempty"`
}

// ErrorData — TypeError.
type ErrorData struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	// Recovered marks an error the RUN ITSELF handled and kept working past — a cut stream whose
	// prefix stands, a repetition loop magi aborted on purpose. It is recorded because it happened,
	// not because the turn is over, and a reader that stops at the first error event stops in the
	// middle of a working turn. The headless CLI did exactly that: the loop's "a cut stream is not
	// a failed request" recovery was undone one layer up, and a nearly-finished build-pmars trial
	// exited 1 three seconds into an otherwise healthy reply (TB 2.1, 2026-08-16).
	Recovered bool `json:"recovered,omitempty"`
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
	// Epoch is the guard's mutation count when this council was convened — how many real file
	// changes the turn had made by then. Turn-local and meaningless as a display fact; it is here
	// because the record has to be able to answer "was there work between these two councils",
	// and Changes cannot answer it. Changes is CLIPPED (councilDiffCap) for the members to read,
	// so two councils that edited different files can carry byte-identical Changes whenever the
	// edits land past the clip — and the short-circuit that reads them then tells a turn that did
	// real work it made "no new edits". The counter that already means exactly this is the one the
	// rejection cap reads two lines below it; recording it makes the log say the same thing.
	Epoch int `json:"epoch,omitempty"`
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
	// Silent marks a verdict nobody gave — backend down, deadline, or a reply that could not be
	// read. It rides beside decision "abstain" so a surface can say "no answer" where a member
	// never spoke, instead of reporting a failure as a considered abstention.
	Silent bool `json:"silent,omitempty"`
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
func truncRunes(s string, n int) string { return text.Clip(s, n) }

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
	Kind      session.PartKind `json:"kind"`
	Text      string           `json:"text"`
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
	// Diff is what approving would change, as a unified-ish diff, for the edit-class calls whose
	// arguments already say so (change.EditDiff). A viewer with a diff pane (the IDE) shows the
	// change itself instead of the arguments that imply it; empty for everything else, and never
	// computed by the viewer — two renderings of one edit is how an approval screen comes to show
	// something the tool will not do.
	Diff string `json:"diff,omitempty"`
}

// QuestionRequestedData — TypeQuestionRequested: the agent asks the USER to pick
// among options (the ask_user tool). Index/Total sequence a multi-question call
// (questions are presented one modal at a time, in order).
type QuestionRequestedData struct {
	CallID   string   `json:"callId"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	// Report is the grounds the person is meant to decide on, in the order the decision-report
	// skill asked for them. Recorded rather than transient: a decision's reasons are the part
	// somebody comes back to a month later asking why the fleet went the way it did.
	Report []report.Filled `json:"report,omitempty"`
	Index  int             `json:"index"` // 1-based position within the call
	Total  int             `json:"total"`
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
