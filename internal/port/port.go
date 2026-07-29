// Package port defines the interfaces the core depends on (hexagonal "ports").
// The core and application layers depend only on these; adapters implement them.
// Dependency direction is always inward: adapter -> port <- app/core.
package port

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sayaya1090/magi/internal/core/artifact"
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

// ProviderEvent is one normalized item from an LLM stream.
type ProviderEvent struct {
	Type     ProviderEventType
	Text     string
	ToolCall *session.ToolCall
	Usage    *event.Usage
	Err      error
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
// (Report) against the CONTRACT (Plan/Task) using EVIDENCE (Signals/Diff).
type DeliberationRequest struct {
	Round int // 1-based council round within the turn
	// Phase selects what the council judges and which member prompt is used:
	// "" / "terminate" is the turn-termination gate (default); "plan" is the
	// pre-flight plan audit, where members judge the PROPOSED PROCEDURE against the
	// task — there is no Report, Changes, or Signals yet.
	Phase string
	// NoChanges marks a pure read-only / investigation / answer turn: the agent made no
	// file edits (via its tools) and no signals ran. Such a turn has no artifact to verify
	// and no false success to guard against, so members should approve (done) on a
	// reasonable report rather than demand edits that were never going to exist.
	NoChanges bool
	Task      string // the user's original goal/request
	Plan      string // acceptance criteria / contract, or the proposed procedure when Phase=="plan"
	Report    string // the agent's self-reported result / claim (optional)
	// Actions is a summary of this turn's tool RESULTS (e.g. write "wrote 13 bytes to
	// hello.txt", bash `cat` output) — real, git-independent evidence so the council can
	// judge a create/write turn in a non-git workdir on what happened, not on an absent
	// diff. It excludes the model's own narration (that is the Report/claim); admitting
	// narration as evidence is how a defeatist agent talks its way to a false "done".
	Actions string
	Signals []Signal         // deterministic evidence (build/test/lint outcomes), optional
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
	// Keep asks each member to ALSO report, alongside its fix feedback, what the report
	// already gets right through its lens — advisory "keep this, don't redo/revert it" that
	// is surfaced above the feedback when the turn continues. It never affects the decision
	// or tally. Off → members are not asked and no keep is produced (MAGI_COUNCIL_KEEP).
	Keep bool
}

// Signal is a piece of deterministic evidence the council weighs (D16): the
// outcome of a verifiable check, so a member doesn't take the agent's claim on
// faith. The verification lens in particular treats a failing signal as strong
// grounds to continue.
type Signal struct {
	Source string // who produced it, e.g. "verify"
	Kind   string // "test" | "build" | "lint" | ...
	Status string // "pass" | "fail"
	Detail string // short output excerpt (tail)
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

// ---- Tools ----

// Tool is an executable capability exposed to the agent. Built-in tools, Lua
// plugin tools, and MCP-bridged tools all satisfy this interface.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage, env ToolEnv) (session.ToolResult, error)
}

// ReportInput is a subagent's structured final report — the output side of the delegation
// contract, mirroring the curated brief it was given. Only Status is required; the rest close
// the loop on the delegated deliverable so the orchestrator/council can judge on evidence:
// Summary/Details carry the result narrative, Evidence is the verification that proves "done",
// Deviations surfaces assumptions/workarounds/unmet boundaries (management-by-exception), and
// Handoff hands the next step the facts it needs.
type ReportInput struct {
	Summary    string
	Status     string
	Details    string
	Evidence   string
	Deviations string
	Handoff    string
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
	// EmitArtifact lets a tool publish a reviewable artifact (D11).
	EmitArtifact func(artifact.Artifact)
	// EmitProgress lets a long-running tool publish a live, best-effort progress
	// note (e.g. wait_for's poll status) while it blocks, so the TUI spinner and
	// the headless stream can show what is being waited on instead of a silent
	// gap. Transient and droppable; always set by the application, but a tool
	// should still nil-check it (a bare ToolEnv has no observer).
	EmitProgress func(text string)
	// Ask lets a running subagent request something from its orchestrator
	// mid-task (escalation); it blocks until the orchestrator replies. Set only
	// for subagents; nil for the top-level agent.
	Ask func(question string) (string, error)
	// AskUser presents a multiple-choice question to the HUMAN user and blocks
	// for the pick (top-level interactive sessions only; nil otherwise — the
	// tool then tells the model to proceed on its own judgment).
	AskUser func(question string, options []string) (string, error)
	// Report is how a subagent delivers its FINAL result and ends its turn:
	// status is "done" | "blocked" | "failed". Set only for subagents. Returns an
	// error if called by a non-subagent or after a report was already filed.
	Report func(ReportInput) error
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
	// (remember{scope:"turn"}). Verbatim in, verbatim out; nil when unavailable.
	NoteForTurn func(text string)
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

// PluginHost loads, reloads, and unloads plugins and exposes their capabilities.
type PluginHost interface {
	Load(ctx context.Context, dir string) (PluginInfo, error)
	Unload(name string) error
	Reload(name string) error
	Capabilities() CapabilitySet
}

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

// ---- Scheduler (D12: Tier1 in-process ticker; Tier2 OS adapter later) ----

// Scheduler triggers agents/commands on a schedule.
type Scheduler interface {
	Schedule(spec ScheduleSpec, target Trigger) (id string, err error)
	Cancel(id string) error
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
