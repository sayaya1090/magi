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
	"github.com/sayaya1090/magi/internal/core/session"
)

// ---- LLM (D3: OpenAI-compatible adapter is the first implementation) ----

// LLMProvider streams a chat completion, normalizing any provider's stream into
// a channel of ProviderEvent. The channel is closed when the stream ends.
type LLMProvider interface {
	StreamChat(ctx context.Context, r ChatRequest) (<-chan ProviderEvent, error)
}

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

// ---- Tools ----

// Tool is an executable capability exposed to the agent. Built-in tools, Lua
// plugin tools, and MCP-bridged tools all satisfy this interface.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage, env ToolEnv) (session.ToolResult, error)
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
	// AskPermission gates dangerous operations; returns true if allowed.
	AskPermission func(callID, name string, args json.RawMessage) (bool, error)
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
	// EmitProgress lets a long-running tool publish a live, best-effort progress
	// note (e.g. wait_for's poll status) while it blocks, so the TUI spinner and
	// the headless stream can show what is being waited on instead of a silent
	// gap. Transient and droppable; always set by the application, but a tool
	// should still nil-check it (a bare ToolEnv has no observer).
	EmitProgress func(text string)
	// AskUser presents a multiple-choice question to the HUMAN user and blocks
	// for the pick (top-level interactive sessions only; nil otherwise — the
	// tool then tells the model to proceed on its own judgment).
	AskUser func(question string, options []string) (string, error)
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
	// NoteForTurn stores one thing the agent asked to be reminded of before this turn ends
	// (remember{scope:"turn"}). Verbatim in, verbatim out; nil when unavailable. Returns the
	// reason the note was NOT kept (the queue is bounded), or nil when it was — a caller that
	// answers "noted" on a discarded note tells the agent it has a reminder it will never get.
	NoteForTurn func(text string) error
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
	Platform     Platform
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
	Retrieve(ctx context.Context, query string) ([]Memory, []Skill, error)
	Propose(ctx context.Context, c Contribution) error // goes to a review queue
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
	Source   string
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
}

// SpawnResult is what the child left behind. Failure is reported, never swallowed: a caller told
// nothing would read silence as success.
type SpawnResult struct {
	SessionID string // the child's session id, so its log can be read afterwards
	Text      string // the child's final message
	Steps     int
	Err       string // why the child stopped short, empty when it finished cleanly
}

// ToolMetadata is what a tool says about itself beyond its schema. Optional: a tool that has
// nothing to add does not implement MetaTool and gets the zero value, which is the behaviour every
// built-in has always had.
type ToolMetadata struct {
	// Subagent marks a tool that runs a child agent. It puts the tool in the /subagents list and
	// forces it to run alone: a child writes files, and the parent's guard captures each file's
	// before/after around an edit, which is only race-free when writes are serialised.
	Subagent bool
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
