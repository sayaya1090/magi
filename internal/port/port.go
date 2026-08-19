// Package port defines the interfaces the core depends on (hexagonal "ports").
// The core and application layers depend only on these; adapters implement them.
// Dependency direction is always inward: adapter -> port <- app/core.
package port

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"
)

// ---- LLM (D3: OpenAI-compatible adapter is the first implementation) ----

// LLMProvider streams a chat completion, normalizing any provider's stream into
// a channel of ProviderEvent. The channel is closed when the stream ends.
type LLMProvider interface {
	StreamChat(ctx context.Context, r ChatRequest) (<-chan ProviderEvent, error)
}

// A provider is more than the one method above, and the extras are where wrappers go wrong.
//
// StreamChat is the only thing every backend must do, so it is the only thing the port demands.
// The rest — listing a catalog, measuring a window, being pointed somewhere else — is offered by
// some backends and not others, and callers reach it with a type assertion. That worked until
// somebody wrapped a provider: a wrapper implements the port, satisfies the assertion's target
// type never, and the caller reads the miss as "this backend cannot do that". Measured: the
// console's model menu came back empty for weeks with a backend behind it that listed three
// models to a plain curl.
//
// Naming them makes the obligation checkable. Every wrapper in this tree carries a compile-time
// assertion against ProviderExtras, and internal/arch fails the build if a new one appears
// without it — so a capability can be added to a backend without being silently dropped one layer
// up.
type ModelLister interface {
	// ListModels returns the backend's catalog. ErrCapabilityAbsent when the backend does not
	// answer that question — which is NOT the same as an empty catalog, and callers that fold the
	// two together end up offering nothing where they should be saying "this one cannot say".
	ListModels(ctx context.Context) ([]string, error)
}

type ContextProber interface {
	// ProbeContextWindow reports a model's real window. ok=false means unknown.
	ProbeContextWindow(ctx context.Context, model string) (int, bool)
}

type BaseRedirector interface {
	// SetBaseURL points the backend elsewhere and returns a token to release the override with.
	SetBaseURL(url string) uint64
	ClearBaseURL(tok uint64)
	// BaseURL is where requests go RIGHT NOW — the override when one is installed, else what the
	// config named. A setter without a reader is a control a second viewer can only fire blind:
	// the console lists the backends that are serving and, without this, could not say which of
	// them is the one in use, so its provider select opened blank on every companion.
	BaseURL() string
}

// ProviderExtras is the whole optional surface, as one name a wrapper can be held to.
type ProviderExtras interface {
	ModelLister
	ContextProber
	BaseRedirector
}

// ErrCapabilityAbsent is what an optional capability answers when the backend underneath does not
// have it. It exists so "I cannot say" stops arriving as "there is nothing" — the shape that made
// an empty model menu indistinguishable from a backend that never got asked.
var ErrCapabilityAbsent = errors.New("this backend does not offer that")

// ChatRequest is a provider-agnostic chat completion request.
type ChatRequest struct {
	Model    string
	System   string
	Messages []session.Message
	Tools    []ToolSpec
	// Params carries per-call sampling pins: "temperature", "top_p", "top_k". A pin outranks the
	// configured default ([sampling]) for that field only; an unrecognised or unusable key is
	// ignored, not an error. The output cap is NOT here — it is provider config, applied by the
	// adapter itself.
	Params map[string]any
}

// ToolSpec describes a tool to the model (name, description, JSON schema).
type ToolSpec struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// ProviderEventType discriminates a ProviderEvent.
type ProviderEventType string

const (
	ProviderText      ProviderEventType = "text-delta"
	ProviderReasoning ProviderEventType = "reasoning-delta"
	ProviderToolCall  ProviderEventType = "tool-call"
	ProviderFinish    ProviderEventType = "finish"
	ProviderUsage     ProviderEventType = "usage"
	ProviderError     ProviderEventType = "error"
)

// ErrStreamCut marks the one provider failure that is not a failed request: the stream carried a
// reply and then ended with neither finish_reason nor [DONE]. What arrived is a PREFIX of an
// answer — the same fact a finish_reason of "length" states, arrived at by a dropped connection
// instead of a token budget — so the loop treats it that way rather than ending the run. Callers
// test it with errors.Is; the message stays on the wrapped error so the log still reads plainly.
var ErrStreamCut = errors.New("the model stream ended without finishing")

// ErrStreamAborted marks a stream THIS PROCESS ended on purpose: the provider guard saw a
// degenerate repetition loop and cut the connection. Without its own sentinel the abort reached
// the loop as a bare "context canceled" — indistinguishable from a transport failure — so the
// safety net ended the run it was meant to save (regex-log, TB 2.1 2026-08-16: the guard fired 19
// seconds into the first reply and the task was lost whole). What was streamed before the abort
// is a prefix, exactly as with ErrStreamCut, so the loop treats the two the same way.
var ErrStreamAborted = errors.New("magi stopped the model stream")

// ProviderEvent is one normalized item from an LLM stream.
type ProviderEvent struct {
	Type     ProviderEventType
	Text     string
	ToolCall *session.ToolCall
	Usage    *event.Usage
	Err      error
	// FinishReason is the provider's own finish_reason, carried verbatim on a
	// ProviderFinish ("stop", "length", "tool_calls", …). The value used to be
	// checked for presence and thrown away, which made a reply CUT OFF at the
	// output-token cap arrive in the exact shape of a complete one — the same
	// defect the council's drain() reports for a cut stream, one layer down.
	FinishReason string
	// FromText is set on a tool-call that was recovered from the assistant's
	// text output (prompt-based fallback) rather than native tool_calls. The
	// loop uses this to avoid also persisting the text as a separate part.
	FromText bool
}

// ---- Council (D14: consensus termination gate) ----

// Council deliberates at the loop's termination gate: it polls the members
// (e.g. each over an LLMProvider) and returns the tallied decision. The
// consensus logic itself is the pure core/council package; an implementation of
// this port supplies the I/O (asking each member, parsing their verdict).
type Council interface {
	Deliberate(ctx context.Context, req DeliberationRequest) (council.Deliberation, error)
}

// DeliberationRequest is the evidence the council judges: the agent's CLAIM
// (Report) against the CONTRACT (Plan/Task) using EVIDENCE (Actions/Changes).
type DeliberationRequest struct {
	Round int // 1-based council round within the turn
	// NoChanges marks a pure read-only / investigation / answer turn: the agent made no
	// file edits (via its tools). Such a turn has no artifact to verify
	// and no false success to guard against, so members should approve (done) on a
	// reasonable report rather than demand edits that were never going to exist.
	NoChanges bool
	Task      string // the user's original goal/request
	Plan      string // acceptance criteria / contract
	Report    string // the agent's self-reported result / claim (optional)
	// Actions is a summary of this turn's tool RESULTS (e.g. write "wrote 13 bytes to
	// hello.txt", bash `cat` output) — real, git-independent evidence so the council can
	// judge a create/write turn in a non-git workdir on what happened, not on an absent
	// diff. It excludes the model's own narration (that is the Report/claim); admitting
	// narration as evidence is how a defeatist agent talks its way to a false "done".
	Actions string
	Changes string           // this turn's file edits, reconstructed from the agent's write/edit tools (optional)
	Members []council.Member // who votes (defaults to the MAGI when empty)
	Rule    council.Rule     // how votes are tallied (defaults to majority)
	// DefaultModel is used for members that don't pin their own Model (typically
	// the session's current model, so the council follows model switches).
	DefaultModel string
	// Debate enables the disagreement-triggered rebuttal round: after the members
	// vote INDEPENDENTLY (round 1 above, preserving uncorrelated errors), if they
	// split (both done and continue present), each member is shown the others'
	// verdicts+rationales ONCE and may hold or change its vote; the re-tally is the
	// result. Consensus → one outcome; still split → the ordinary rule decides.
	// No extra call when the independent vote is unanimous (the common case).
	Debate bool
	// OnVerdict, when set, is called with each member's verdict the moment that member
	// answers, from the goroutine that polled it — so a caller can show a council as it
	// lands instead of after the slowest member. Members are polled concurrently and one
	// can take minutes, so batching the three costs the whole wait before anything appears.
	// It is a NOTIFICATION, not the result: the returned Deliberation is still the record,
	// and a verdict revised by the rebuttal round is not re-announced through here.
	// Implementations must be safe to call from several goroutines.
	OnVerdict func(council.Verdict)
	// Keep asks each member to ALSO report, alongside its fix feedback, what the report
	// already gets right through its lens — advisory "keep this, don't redo/revert it" that
	// is surfaced above the feedback when the turn continues. It never affects the decision
	// or tally. Off → members are not asked and no keep is produced (MAGI_COUNCIL_KEEP).
	Keep bool
}

// ---- Store (D6: event-sourced persistence; jsonl is the first impl) ----

// Store persists and reads the per-session event log. Append assigns a per-
// session monotonically increasing seq (starting at 1) to each fact event and
// returns the assigned seq values in order.
type Store interface {
	Append(ctx context.Context, s session.SessionID, evs ...event.Event) ([]int64, error)
	Read(ctx context.Context, s session.SessionID, fromSeq int64) ([]event.Event, error)
	ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error)
	// ChildSessions returns the subagent (child) sessions spawned by parentID.
	ChildSessions(ctx context.Context, workdir, parentID string) ([]session.SessionMeta, error)
	Compact(ctx context.Context, s session.SessionID, upToSeq int64, snapshot event.Event) error
	// Truncate drops all events with seq > upToSeq (rewind), archiving the
	// original log.
	Truncate(ctx context.Context, s session.SessionID, upToSeq int64) error
}

// Elsewhere is work being done in another session, whose answer belongs back in this one.
//
// It carries no channel and no callback. The answer is written where every answer is written — the
// doer's own transcript — and what is registered here is only the intention to go and read it. So
// nothing is lost if this process dies: the work is still being done, the answer is still written
// down, and `companions` still shows it. What is lost is the delivery, which is the right thing to
// be the fragile half.
type Elsewhere struct {
	// Who is doing it, as the note coming back should name them.
	Who string
	// Session is theirs, which is where the answer will appear.
	Session string
	// Request is what they were asked, so the note that arrives says what it is an answer TO. A
	// turn that handed out three pieces gets three answers back and cannot otherwise tell which is
	// which — they arrive in whatever order the work finishes, not the order it was asked.
	Request string
	// AnswerAs is the form the asker said the answer had to come back in.
	//
	// Carried so the note that delivers their answer can put the two side by side. Whether an
	// answer ARRIVED is known without this; whether it is the thing is a comparison, and the only
	// moment it is cheap to make is when the answer is read.
	AnswerAs string
	// Probe answers "is anybody still doing this", and it is what keeps silence from being
	// ambiguous.
	//
	// A transcript that has stopped growing says nothing about why. The peer may be inside a
	// ten-minute build, or blocked on a permission prompt nobody is at the keyboard for, or its
	// daemon may have been killed with the turn half done — and all three look identical from the
	// log, which is the only thing the waiting side can read. Without this the wait runs to its
	// full two hours and then reports "not finished", which is true of every one of those and
	// useful for none.
	//
	// news is worth passing on now; over says nothing more will ever come, so the wait should
	// stop. Both empty/false means carry on waiting, which is also what an unanswerable probe
	// returns — a probe that cannot tell must not guess that the work is dead.
	//
	// nil is allowed and means exactly that: no way to tell, wait it out.
	Probe func() (news string, over bool)
	// Answer fetches the finished answer, for work whose transcript is not on this disk.
	//
	// Normally there is nothing to fetch: the answer is written where every answer is written and
	// the wait simply reads the doer's log. That is what makes a local hand-off survivable, and it
	// stops at the machine boundary — their log is a directory on their disk.
	//
	// So the same question is asked over the wire instead, and the far side answers it with the
	// same code the local read uses. One question; the way of putting it is chosen by where the
	// log is. nil means the log is here, which is the ordinary case.
	Answer func() (text string, done bool)
	// Ready is closed-or-signalled when Probe and Answer have something new to say.
	//
	// The wait has a clock of its own, sized for reading a log file on this disk. A hand-off
	// across a machine does not read anything: whoever supplies Answer holds a connection to the
	// doer and is TOLD. Without this the news would sit until the next tick — the wait would be
	// pushed to and then poll anyway.
	//
	// A nudge, not the news. What happened is still read through Probe and Answer, so there is one
	// place that decides what an answer is. nil means "no news channel; the clock is all there
	// is", which is the ordinary local case.
	Ready <-chan struct{}
	// Done is called once when the wait is over, however it ended.
	//
	// Whoever supplied Answer may be holding a process open to do it, and the wait is the only
	// thing that knows it has stopped caring — by deadline, by an answer arriving, or by this
	// daemon shutting down. nil means there is nothing to release.
	Done func()
}

// ScheduleChange is one edit to a workspace's unattended work, or a request to see it.
//
// Action is "list", "set" or "remove". On "set", an empty Schedule or Prompt means "leave that part
// of the existing job alone", so the words can be rewritten without restating the time. Enabled is
// a pointer because absent means "do not touch it" — distinct from switching a job off.
type ScheduleChange struct {
	Action   string
	Name     string
	Schedule string
	Prompt   string
	Enabled  *bool
}

// ChildStep is one tool call a child made: what it ran, with what arguments, and whether it worked.
//
// Output carries the tool's result VERBATIM for a FAILED call and is empty for one that succeeded.
// That split is the whole design. The raw stderr of a failed build is the thing a caller cannot
// reconstruct and cannot afford to have summarised — it is why the loop is running another round.
// Successful output is the bulk of a session's bytes and the caller already knows the shape of it
// from Name and Args, so carrying it would turn "the footprint" back into "the whole log", which is
// what this exists instead of. OutputBytes says how much was left behind either way.
type ChildStep struct {
	Name        string          // the tool the child called
	Args        json.RawMessage // its arguments, exactly as the child sent them
	Failed      bool
	Output      string // verbatim, and only when Failed
	OutputBytes int    // size of the result, set whether or not Output is carried
}

// RestoredPath is one file a restore tried to put back. How is "journal" (magi wrote the original
// bytes back), "git" (recovered from the index), "saga" (the change was undone rather than
// rewritten — a file the child created was removed), or empty when nothing worked, in which case
// Reason says why.
type RestoredPath struct {
	Path     string
	Restored bool
	How      string
	Reason   string
}

// ---- Tools ----

// Tool is an executable capability exposed to the agent. Built-in tools, Lua
// plugin tools, and MCP-bridged tools all satisfy this interface.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage, env ToolEnv) (session.ToolResult, error)
}

// Question is one thing put to a person, with everything a surface needs to draw it whole.
//
// A struct rather than a longer parameter list: the position arrived after the text and the
// grounds did before it, and a signature that grows by appending is one every implementation has
// to be edited to ignore.
type Question struct {
	Text    string
	Options []string
	// Grounds is the report the person decides ON: what was tried, what each option costs, which
	// way the agent leans. Its sections are whatever the decision-report skill declares.
	Grounds []report.Filled
	// Index and Total place it in the run of questions this one call is asking, counting from one.
	// Total is 1 for the ordinary case and never 0 — a surface should not have to treat "unknown"
	// as a third state.
	Index, Total int
}

// ToolEnv carries per-execution context and capabilities granted to a tool.
type ToolEnv struct {
	SessionID session.SessionID
	Workdir   string
	// ScratchLogs is the TURN's directory for captured command output. A tool that runs a command
	// writes the combined stdout/stderr there and names the file in its result, so the output
	// outlives the call: the elided middle of a large capture is still on disk, and a later step
	// can grep the part it needs instead of re-running with a bigger tail. Empty = capture to a
	// temp file and delete it when the call returns (the pre-scratch behavior).
	ScratchLogs string
	// ScratchTmp is the TURN's temp directory, exported to the command as TMPDIR. Everything that
	// ASKS for a temp path — mktemp, python's tempfile, a compiler's intermediates — then writes
	// outside the deliverable tree without the model knowing anything about it. Removed whole when
	// the turn ends. Empty = the process's own TMPDIR.
	ScratchTmp string
	// Spawn runs a child agent to completion and returns what it produced. nil when the host does
	// not offer it — and it is nil inside a child, which is what makes recursion impossible rather
	// than merely discouraged.
	Spawn func(ctx context.Context, spec SpawnSpec) (SpawnResult, error)
	// ChildSteps returns what a child SPAWNED BY THIS TOOL CALL actually did: one entry per tool
	// it ran, in order. nil when the host does not offer it.
	//
	// A child's final message is the child's own account of its work, and this tree has measured
	// what that costs — a brief paraphrased until the identifiers were gone (2bd1fb6). The
	// footprint is not an account; it is the calls themselves. A caller can tell a child that ran
	// the build and watched it fail from one that never ran it, which the closing sentence of
	// either child looks identical for.
	//
	// It answers only for this call's own children. A plugin that could name any session id could
	// read another agent's log, and nothing about spawning gives it a reason to.
	ChildSteps func(ctx context.Context, sessionID string) ([]ChildStep, error)
	// RestoreChild puts back the files a child of THIS tool call changed, and reports every path
	// either way. nil when the host does not offer it.
	//
	// Best-effort, and the report is the point: what could not be put back is named, with the
	// reason. Half a restore reported as a clean one is worse than none — the next round of a loop
	// would build on a tree it believes is clean.
	RestoreChild func(ctx context.Context, sessionID string) ([]RestoredPath, error)
	// MergeChild applies a child's work onto the parent's tree, for a child that had its own
	// checkout. It is the commit range the child produced, applied with git's own three-way merge,
	// so a conflict comes back as conflict markers and not as a silent overwrite. Nothing is
	// committed in the parent — what lands is a working-tree change for a human to read.
	MergeChild func(ctx context.Context, sessionID string) error
	// EmitProgress lets a long-running tool publish a live, best-effort progress
	// note (e.g. wait_for's poll status) while it blocks, so the TUI spinner and
	// the headless stream can show what is being waited on instead of a silent
	// gap. Transient and droppable; always set by the application, but a tool
	// should still nil-check it (a bare ToolEnv has no observer).
	EmitProgress func(text string)
	// AskUser presents a multiple-choice question to the HUMAN user and blocks
	// for the pick (top-level interactive sessions only; nil otherwise — the
	// tool then tells the model to proceed on its own judgment).
	//
	// grounds is the report the person decides ON: what was tried, what each option costs, which
	// way the agent leans. Its sections are whatever the decision-report skill declares, so what a
	// report contains is the operator's choice and differs per companion. A question used to arrive
	// as a sentence and two labels, and the person was asked to choose with none of what the agent
	// had spent an hour learning.
	//
	// at says which question this is and how many are coming: a tool may ask several in a row, and
	// each one blocks. Without it the person answers the first not knowing there are two more —
	// which changes the answer, because "is this the whole decision" is part of the decision.
	AskUser func(q Question) (string, error)
	// Council asks the configured council for a reading of the work so far and returns their
	// answers as text. complete marks the call as a DECLARATION that the task is finished: the
	// council reads the record as a finish, and if it accepts, the application ends the turn.
	// Set for any session with a council configured; nil leaves the tool reporting that there is
	// none rather than pretending to have asked.
	Council func(ctx context.Context, question string, complete bool) (string, error)
	// RouteInterjection routes a user request that arrived mid-turn: action is
	// "queue" (default — run it after the current task), "redirect" (switch to it
	// now), or "append" (fold it into the current task). requestID names which pending
	// request to route (the [req: …] handle shown in the directive; "" = the oldest
	// pending). Set ONLY for the top-level orchestrator (depth 0); nil for subagents,
	// which the user does not steer.
	RouteInterjection func(action, reason, requestID string) error
	// SetTodos replaces the session's plan (TodoWrite); nil when unavailable.
	SetTodos func(todos []session.Todo)
	// SetLabels replaces what the agent says this work is about; nil when unavailable. The whole
	// set each time, for the same reason SetTodos takes the whole plan: a reader that has to fold
	// a stream of adds and removes is a reader that is wrong the first time one is missed.
	SetLabels func(labels []string)
	// NoteForTurn stores one thing the agent asked to be reminded of before this turn ends
	// (remember{scope:"turn"}). Verbatim in, verbatim out; nil when unavailable. Returns the
	// reason the note was NOT kept (the queue is bounded), or nil when it was — a caller that
	// answers "noted" on a discarded note tells the agent it has a reminder it will never get.
	NoteForTurn func(text string) error
	// WikiAdvise, given a page title about to be written, answers with a one-line note when a
	// page under a DIFFERENT but overlapping title already exists — the convergence pressure
	// against near-duplicates piling up under drifting names. "" when there is nothing to say;
	// nil when the wiki is unavailable. Advisory only: the write proceeds either way.
	WikiAdvise func(page string) string
	// Propose contributes a memory/skill to the shared experience store (D13);
	// nil when unavailable.
	Propose func(c Contribution) error
	// LoadSkill returns a named skill's instructions; nil when unavailable.
	LoadSkill func(name string) (string, bool)
	// Recall re-hydrates a topic's full detail that an earlier compaction shed from
	// context, looked up by topic/keywords against the compaction shards; nil when
	// unavailable. (recall_context)
	Recall func(query string) (string, error)
	// RecallMemory pulls durable team memories/skills (the shared experience store)
	// that match a query, on demand. Distinct from Recall (which recovers this
	// session's compacted context): this reaches the cross-session D13 store. The
	// prompt only advertises a count; the agent calls this to pull the details, so
	// nothing is spent on context until it actually wants them. nil when the
	// experience store is not configured. (recall_memory)
	RecallMemory func(query string) (string, error)
	// SearchSessions ranks past TURNS in this workspace against a query, and with a non-empty open
	// ("<session>#<seq>", as printed beside every hit) returns that turn whole and verbatim.
	//
	// The third of three recall routes, and the boundaries between them are the whole point: Recall
	// reaches this session's compacted-out detail, RecallMemory reaches the curated store that
	// `remember` writes, and this one reaches the raw sessions of other days — the only one of the
	// three that can find something nobody thought to save. nil when unavailable. (search_sessions)
	SearchSessions func(query, open string) (string, error)
	// Schedule lists or changes the unattended work this workspace does. nil when unavailable.
	//
	// The one tool whose effect outlives the turn that called it: a job set here runs every morning
	// until somebody stops it. Nothing about that is hidden — the job is a named table in the
	// project's committable config, every surface lists it, and the daemon names them all at
	// startup. (schedule)
	Schedule func(ScheduleChange) (string, error)
	// Expect says a piece of this task is now being done in ANOTHER session, and its answer is to
	// be brought into this conversation when it lands. nil when unavailable. (hand_off)
	//
	// It is what makes handing work over asynchronous instead of a dead end. Before it, a request
	// crossed to another companion and the tool's own answer said "they do not report back, so
	// read their transcript later" — so the asker either stopped and waited on a screen it could
	// not see, or carried on and forgot. Registering the wait here means the asker keeps working
	// and the reply arrives in its conversation the way a steer does.
	Expect   func(Elsewhere) error
	Platform Platform
	// Sandbox requests OS-level confinement for commands (bash). Zero value
	// (empty Mode) means unconfined.
	Sandbox SandboxSpec
}

// SandboxSpec describes OS-level confinement for command execution (an OS
// sandbox axis). Mode is "read-only" (no writes), "workspace-write" (writes
// limited to Workdir + temp), or "full"/"" (unconfined). AllowNet permits
// outbound network; it is off by default outside "full".
type SandboxSpec struct {
	Mode     string
	Workdir  string
	AllowNet bool
	// ReadOnly are paths that stay read-only even when they fall inside something the mode makes
	// writable. One user so far and it is the important one: the session store lives under the
	// user's cache directory, and the cache directory is writable so build tools keep working — so
	// a confined command could rewrite or delete any transcript on the machine, including the one
	// recording what it was doing.
	ReadOnly []string
	// TempDir, when set, NARROWS the temp allowance to this one directory instead of the
	// platform's shared temp trees. The shared trees are the right default for an operator's own
	// confined run — build tools write temp constantly — and an open door for an ISOLATED CHILD:
	// /tmp is world-shared, and the per-user temp tree holds every sibling's clone. Observed live
	// (the escape-attempt wave run, 2026-08-16): a child's `echo … > /tmp/…/two.txt` sailed through
	// the seatbelt because /tmp was on the allow list. The bash tool already points TMPDIR at the
	// turn's scratch, so tooling keeps working when this is that scratch directory.
	TempDir string
}

// Confined reports whether the spec requests actual confinement.
func (s SandboxSpec) Confined() bool {
	return s.Mode == "read-only" || s.Mode == "workspace-write"
}

// ToolRegistry holds the set of available tools by name.
type ToolRegistry interface {
	Register(Tool)
	Get(name string) (Tool, bool)
	List() []Tool
}

// ---- Context providers ----

// ContextProvider injects relevant context during prompt assembly.
type ContextProvider interface {
	Provide(ctx context.Context, q ContextQuery) ([]ContextChunk, error)
}

// ContextQuery describes what context is being assembled for.
type ContextQuery struct {
	SessionID session.SessionID
	Workdir   string
	Prompt    string
}

// ContextChunk is a labeled piece of injected context.
type ContextChunk struct {
	Source string
	Text   string
}

// ---- Doctor probes ----

// DoctorProbe is an environment check a plugin contributes to `magi -doctor`
// (e.g. an SSO plugin verifying its cached token isn't expired). Run reports a
// status — one of "ok", "warn", "fail", "info" — and a human-readable detail; an
// unrecognized status is normalized to "info" by the doctor command. Probes must
// be self-contained (read cached/persistent state) since -doctor loads plugins
// without firing their startup handlers.
type DoctorProbe interface {
	Name() string
	Run(ctx context.Context) (status, detail string)
}

// ---- Shared experience (D13: team brain, git-repo backed) ----

// ExperienceStore is the shared, curated memory+skill store for a team.
type ExperienceStore interface {
	// Retrieve returns what best matches the query, narrowed to what this caller may see.
	//
	// agentGroups is the ASKING AGENT's groups. A skill that declares `agent-groups` is offered
	// only when the two sets intersect; a skill that declares none is offered to everyone, which
	// is what keeps every skill written before the field existed working. An agent with no groups
	// therefore sees exactly the unlabelled ones — the other way round, labelling a skill would
	// not shrink anybody's context, and shrinking it is the reason to label one.
	Retrieve(ctx context.Context, query string, agentGroups []string) ([]Memory, []Skill, error)
	Propose(ctx context.Context, c Contribution) error // goes to a review queue
}

// ---- Shared wiki (canonical, update-in-place team knowledge) ----

// WikiPage is one canonical page: the CURRENT truth about its topic, updated in place. It exists
// for the thing the append-only experience store cannot hold — three companions working the same
// system for days, each re-deriving structure the others already mapped. A fact that changed gets
// corrected here, not contradicted beside itself; a page that stopped being true is marked stale
// with the reason, never deleted, because "no longer so, and here is why" is knowledge too.
type WikiPage struct {
	Title   string   // the topic's identity — also the dedup anchor
	Body    string   // the current content (the winning revision's snapshot)
	Links   []string // titles of related pages
	Stale   bool     // marked no-longer-true; the body stays as the record of what was believed
	Tier    string   // where the winning revision lives: "project" | "team" | "global"
	Updated string   // RFC3339 of the last edit
	Editor  string   // who edited last (companion/source)
	Summary string   // the last edit's summary — what changed and why
}

// WikiEdit is one page write riding a Contribution: create or update Page in place. Text is the
// page's NEW full body (a snapshot, not a diff — pages are small and snapshots merge by union).
// Summary says what changed; empty is tolerated and falls back to the first line of Text, because
// a refused write teaches a weak model to stop writing at all.
type WikiEdit struct {
	Page    string
	Text    string
	Links   []string
	Summary string
	// Stale retires the page: the write lands as a tombstone revision whose body is the REASON
	// it stopped being true. The page leaves the index, is demoted in search, and stays readable
	// — deletion never exists, on the wire or off it.
	Stale bool
}

// WikiStore is the read side of the canonical-page store. OPTIONAL: the app type-asserts it off
// the ExperienceStore, so a store that predates the wiki keeps working untouched. Writes ride
// Contribution.Wiki through Propose — one write seam, not two.
type WikiStore interface {
	// WikiSearch returns up to n pages matching the query, best first — an exact or near title
	// match outranks body matches, which is what makes a title query behave as a page fetch.
	WikiSearch(ctx context.Context, query string, n int) ([]WikiPage, error)
	// WikiIndex returns up to n page titles with a one-line hook each, most recently edited
	// first — what a context block advertises so a model learns the wiki exists without paying
	// for its bodies.
	WikiIndex(ctx context.Context, n int) ([]WikiPage, error)
}

// Memory is a learned fact/convention/pitfall.
type Memory struct {
	ID   string
	Text string
	Tags []string
}

// Skill is a reusable, named procedure.
type Skill struct {
	Name        string
	Description string
	Body        string
}

// Contribution is a proposed addition to the shared experience.
type Contribution struct {
	Memories []Memory
	Skills   []Skill
	// Wiki carries canonical-page edits (remember{page:…}). Unlike Memories these update in
	// place; Scope routes them the same way, except the wiki's default tier is TEAM — a page is
	// written to be read by the other companions, and a workspace-local default would file it
	// where they never look.
	Wiki   []WikiEdit
	Source string
	// Scope selects the tier a layered store writes to: "global" for cross-project
	// knowledge, "" or "project" (the default) for workspace-local learnings.
	Scope string
}

// ---- Plugin host (D10: hot-reloadable capability bundles) ----

// PluginInfo summarizes a loaded plugin.
type PluginInfo struct {
	Name         string
	Version      string
	Capabilities []string
}

// PluginCommand is a slash command contributed by a plugin (e.g. /login). It is
// modeled as an interface — like Tool and ContextProvider — so the owning plugin
// can serialize Execute on its (non-concurrency-safe) Lua state.
type PluginCommand interface {
	Name() string                // command name without the leading slash (e.g. "login")
	Description() string         // shown in /help and the slash palette
	Execute(args []string) error // args = whitespace-split tokens after the command
}

// CapabilitySet is the aggregate of capabilities contributed by all plugins.
type CapabilitySet struct {
	Tools            []Tool
	ContextProviders []ContextProvider
	Commands         []PluginCommand
	// Skills, Hooks, MCPServers, Agents, UIPanels added in M3+.
}

// ScheduleSpec describes when to fire.
type ScheduleSpec struct {
	Every string // duration (Tier1) or cron expr (Tier2)
}

// Trigger describes what to fire.
type Trigger struct {
	Kind string // "agent" | "command"
	Name string
	Args json.RawMessage
}

// ---- Platform (cross-platform abstraction; §9.5) ----

// Platform abstracts OS-specific behavior so the core stays OS-agnostic.
type Platform interface {
	Exec(ctx context.Context, c Cmd) (ExecResult, error)
	ConfigDir() string
	DataDir() string
	TerminalCaps() TermCaps
	// ProcessCPUTime returns the process's cumulative CPU time (user+system) and
	// whether it is alive. The subagent-lease supervisor samples it across a short
	// window to tell an actively-working background process (CPU advancing) from a
	// wedged or idle one, so a genuine long transfer/build is extended instead of
	// killed as churn. Best-effort: an unreadable or dead pid returns (0, false).
	ProcessCPUTime(pid int) (time.Duration, bool)
}

// Cmd is a command to execute.
type Cmd struct {
	Path  string
	Args  []string
	Dir   string
	Env   []string
	Stdin []byte
	// MaxOutput, when > 0, caps the bytes captured for stdout and for stderr each;
	// output beyond it is discarded (the child keeps running, so it isn't blocked by
	// a full pipe) so an unbounded producer can't grow capture to OOM. Zero = no cap.
	MaxOutput int
}

// ExecResult is the outcome of running a Cmd.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// TermCaps reports detected terminal capabilities (D8).
type TermCaps struct {
	TrueColor bool
	Image     string // "kitty" | "iterm2" | "sixel" | "" (fallback to half-block)
}

// SpawnSpec describes a child agent a TOOL asks the host to run. It is written by whoever
// registered that tool — a plugin — not improvised by the model.
//
// That distinction is the whole reason this exists. The delegation machinery that came out of this
// tree let the model hand part of its own context to a stranger, and the record showed no run it
// made better. What a plugin declares is a different thing: a specialist with a system prompt and a
// model somebody chose on purpose. magi ships none; the seam is unreachable until a plugin
// registers a tool that uses it.
type SpawnSpec struct {
	// ToolName is the tool the child is running on behalf of, for display. The host fills it in —
	// a plugin naming its own tool could name any tool, and the strip would say the wrong thing.
	ToolName string

	System string // the child's system prompt
	// Prompt is the child's task and is seeded VERBATIM. The first defect the removed machinery
	// was charged with was a brief paraphrased until the graded identifier was gone, so nothing
	// here rewrites, summarises or clips it.
	Prompt   string
	Model    string   // empty = the parent session's model
	Provider string   // named LLM profile; empty = default backend
	Tools    []string // the child's allowlist; empty = whatever the agent spec defaults to
	MaxSteps int      // clamped by the host
	Timeout  time.Duration
	// Review, when set, is asked whether a child that stopped is actually finished — the same
	// relationship the main agent has with the council, one level down.
	//
	// A child today ends by going quiet or by hitting a bound, which is the passive finish this
	// tree took away from the main agent: a child that gave up and one that succeeded come back
	// the same shape. Returning a non-empty string sends it back to work IN THE SAME SESSION with
	// that string as the next instruction, so it keeps everything it has already read — a fresh
	// child re-gathers what the last one knew, and re-reading a repository is most of what a
	// second round would otherwise spend its steps on.
	//
	// Returning "" accepts the ending. The host bounds the rounds and the child's TOTAL steps
	// whatever this says, so a review that never accepts cannot run forever.
	//
	// sessionID is the child being judged. It is passed because the review runs BEFORE Spawn
	// returns, so a caller holding the result has nothing yet — and without it the only thing a
	// review can read is the child's own closing sentence, which is exactly the account that a
	// footprint exists to check.
	Review func(round int, text string, steps int, sessionID string) (string, error)
	// Workspace: "" shares the parent's directory (the default), "clone" gives the child its own
	// checkout of it.
	//
	// Two children writing the same tree is not something to undo afterwards — a collision is two
	// correct writes, not a mistake — so isolation is the only answer, and its own checkout is
	// what a second machine would have had for free. The clone carries the parent's UNCOMMITTED
	// work, because a child handed HEAD while the parent has unstaged edits reads a version of the
	// code nobody is looking at, and it has no way to notice.
	//
	// Opt-in: a read-only child wants the parent's live tree and a clone would cost it a copy to
	// see a staler version of what it already had. Asking for "clone" where it cannot be given
	// FAILS rather than sharing quietly — a caller that asked for isolation and silently did not
	// get it produces the very collision it was avoiding.
	Workspace string
	// Groups are the agent groups the child belongs to, deciding which shared skills reach it.
	// This is the field that lets one plugin serve several specialists off one skill store: a
	// reviewer and a doc writer see different shelves because they ask under different groups.
	Groups []string
}

// SpawnResult is what the child left behind. Failure is reported, never swallowed: a caller told
// nothing would read silence as success.
type SpawnResult struct {
	// Workspace is where the child actually worked. Empty when it shared the parent's directory;
	// otherwise the path of its own checkout, which the CALLER is responsible for taking what it
	// wants out of — magi does not merge it back, because whether a failed attempt's changes are
	// wanted is not magi's judgement to make.
	Workspace string
	// Branch, BaseCommit and HeadCommit describe the child's work as a COMMIT RANGE inside that
	// checkout. BaseCommit pins the parent's tree as the child found it — the parent's own
	// uncommitted changes are below that line, so BaseCommit..HeadCommit is exactly what the child
	// did and nothing the parent already had. That is what makes a merge back a git operation
	// rather than a pile of file copies, and what lets it apply onto a parent that is still dirty.
	Branch     string
	BaseCommit string
	HeadCommit string
	SessionID  string // the child's session id, so its log can be read afterwards
	Text       string // the child's final message
	Steps      int
	Err        string // why the child stopped short, empty when it finished cleanly
}

// ToolMetadata is what a tool says about itself beyond its schema. Optional: a tool that has
// nothing to add does not implement MetaTool and gets the zero value, which is the behaviour every
// built-in has always had.
type ToolMetadata struct {
	// Subagent marks a tool that runs a child agent. It puts the tool in the /subagents list and,
	// unless the tool also declares ReadOnlyChildren, forces it to run alone: a child writes files,
	// and the parent's guard captures each file's before/after around an edit, which is only
	// race-free when writes are serialised.
	Subagent bool
	// ReadOnlyChildren says every child this tool spawns can only look: no writing tool, no shell,
	// nothing that asks a person. Two of those have nothing to race over, so a step that calls this
	// tool twice runs both at once instead of paying for them one after the other.
	//
	// It is not taken on trust. A tool that declares it has its spawns CHECKED at the moment they
	// are made — a spec asking for a tool that can write is refused, naming the tool — so the
	// declaration cannot become false by editing the plugin's execute function. Without that this
	// would be a promise in a comment, and the thing it is promising about is file corruption.
	ReadOnlyChildren bool
	// IsolatedChildren says every child this tool spawns that could write works in its OWN
	// checkout — the host defaults such a spawn to workspace="clone" and confines the child's
	// shell writes there (see spawnChild). Two of those cannot touch one tree, so a step that
	// calls this tool twice runs both at once, the same bargain ReadOnlyChildren strikes —
	// reached by isolation instead of by taking writing away.
	//
	// Like ReadOnlyChildren it is a mechanism, not a promise: the default is applied where the
	// child's workspace is decided (spawnFnFor), so a plugin cannot declare it and then quietly
	// spawn a writer into the shared tree.
	IsolatedChildren bool
	// Group is a plugin-declared label for the /subagents list. Purely for display and bulk
	// toggling — the enabled state lives per tool, never per group, so the two cannot disagree.
	Group string
	// DefaultOff ships a subagent switched off: it appears in the list a user manages, unticked,
	// and nothing spawns until they tick it. Only the user's own choice overrides it.
	DefaultOff bool
	// Internal keeps a tool off the main agent's list. It is advertised only to an agent whose
	// allowlist NAMES it, which in practice means a child a plugin spawned with it — so a plugin
	// can ship a narrow tool (say, one that only runs git) for its own specialist without adding
	// weight to every request the main agent makes.
	Internal bool
}

// MetaTool is implemented by a Tool that carries metadata. Type-asserted where it matters.
type MetaTool interface {
	Tool
	Meta() ToolMetadata
}

// ToolMetaOf returns t's metadata, or the zero value when it declares none.
func ToolMetaOf(t Tool) ToolMetadata {
	if m, ok := t.(MetaTool); ok {
		return m.Meta()
	}
	return ToolMetadata{}
}
