package app

import (
	"context"
	"time"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// AgentSpec defines a named agent: its system prompt, optional tool allowlist
// (empty = all tools), and optional model override. (F-AGENT-MULTI)
type AgentSpec struct {
	Name     string
	System   string
	Tools    []string
	Model    session.ModelRef
	Provider string // named LLM profile (different endpoint/key); empty = default backend
	// Groups are the AGENT GROUPS this agent belongs to, and they decide which shared skills it is
	// offered: a skill declaring `agent-groups` reaches it only when the two sets intersect.
	//
	// Empty means it sees only UNLABELLED skills. That direction is deliberate — if an agent with
	// no groups saw everything, labelling a skill would not shrink anyone's context, and shrinking
	// it is the reason to label one. "*" asks for the whole shelf.
	Groups []string
}

// ProfileDef is a named LLM backend definition (endpoint/key/model + one optional
// custom header), editable in the /route menu and persisted to [llm.profiles.*].
type ProfileDef struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string
	Headers map[string]string
}

// ProviderFactory builds an LLM provider for a profile (injected by main so the
// app stays decoupled from the concrete LLM adapter).
type ProviderFactory func(ProfileDef) port.LLMProvider

// PermissionPersister records a user's "always allow for this project" choice
// as a durable allow rule (the project config's allow list), so the same tool
// doesn't re-prompt on every session. Wired from cmd (which knows the config
// paths); nil = the choice lasts for the session only.
type PermissionPersister interface {
	PersistAllow(rule string) error
}

// SubagentPref is one subagent's user settings. Enabled is a POINTER because nil ("no choice
// made") has to be distinguishable from false: a tool that ships switched ON must stay on until
// somebody says otherwise, and a tool that ships off must stay off.
type SubagentPref struct {
	Enabled  *bool
	Model    string
	Provider string
}

// SubagentPersister writes a /subagents edit back to the config file so it survives a restart. It
// records the CHOICE, either way: a subagent that ships switched off needs "the user turned this
// on" to be a thing the config can say.
type SubagentPersister interface {
	PersistSubagent(name string, pref SubagentPref) error
}

// RoutePersister writes /route editor edits back to the config file so they
// persist across restarts: the session default model, and the named backend
// profiles. (A third method once persisted a per-agent [routing] table; nothing
// ever read that table back, so the method and the table are gone.)
type RoutePersister interface {
	PersistModel(modelID string) error
	PersistProfile(p ProfileDef) error
}

func (s AgentSpec) allows(tool string) bool {
	if len(s.Tools) == 0 {
		return true
	}
	for _, t := range s.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

// Config holds run-time policy for the agent loop and orchestration (D7).
// TurnObserver receives top-level conversation milestones, for observer-style
// integrations (the Lua plugin host forwards these as user_message /
// turn_finished events). Both calls happen on the conversation path, so
// implementations must return immediately (enqueue and go).
//
// TurnFinished carries the turn's STRUCTURAL outcome so observers never have to
// guess success from phrasing (the host already knows):
//
//	verified   — the council itself voted done (evidence-backed completion)
//	unverified — the turn landed but the council never read it (the agent never declared)
//	ungated    — the turn used tools and no consensus gate ran on it at all
//	guard      — reserved: an error event coded loop_guard or stall_guard. NOTHING EMITS
//	             those codes today, so this outcome cannot currently occur — the guards
//	             speak and the turn continues. Kept in the contract rather than removed,
//	             so an observer that already handles it keeps working if a producer
//	             returns, but do not write code that waits for it.
//	error      — the turn ended on an error event
//	done       — plain finish with no council verdict either way (e.g. conversational turn)
//
// Reason carries the unverified reason / guard code / error message ("" otherwise).
// SkillsLoaded lists skills the agent loaded (the skill tool) during the turn, so
// an observer can meter which skills actually get used and with what outcome.
// TurnOutcome is the closed set of structural endings a turn can have. Named and enumerated
// rather than written as loose strings at the one switch that produces them, because the loose
// version drifted: `ungated` shipped and reached FORTY PERCENT of observed turns (measured over
// eighty bench sessions, 2026-08-02) while the contract below listed five outcomes and not that
// one — and listed `guard`, which nothing can produce. A contract that is wrong in both
// directions is worse than a short one.
//
// TestTurnOutcomesAreDocumented holds this list against the contract comment, so a new ending
// cannot ship undocumented. That is the Go stand-in for a closed variant the compiler enforces.
type TurnOutcome = string

const (
	// OutcomeVerified — the council itself voted done (evidence-backed completion).
	OutcomeVerified TurnOutcome = "verified"
	// OutcomeUnverified — the turn landed but the council never read it (the agent never declared).
	OutcomeUnverified TurnOutcome = "unverified"
	// OutcomeUngated — the turn used tools and NO consensus gate ran on it. Not a failure by
	// itself: on a bench wave it is overwhelmingly a turn the harness cut short before the agent
	// declared. It is surfaced rather than folded into "done" so an observer never records an
	// unconfirmed completion as a success.
	OutcomeUngated TurnOutcome = "ungated"
	// OutcomeGuard — reserved: an error event coded loop_guard or stall_guard. NOTHING EMITS
	// those codes today, so this cannot currently occur; the guards speak and the turn continues.
	OutcomeGuard TurnOutcome = "guard"
	// OutcomeError — the turn ended on an error event.
	OutcomeError TurnOutcome = "error"
	// OutcomeDone — plain finish with no council verdict either way (e.g. a conversational turn
	// that called no tools).
	OutcomeDone TurnOutcome = "done"
)

// turnOutcomes is every outcome, for the test that holds them against the contract.
var turnOutcomes = []TurnOutcome{
	OutcomeVerified, OutcomeUnverified, OutcomeUngated,
	OutcomeGuard, OutcomeError, OutcomeDone,
}

type TurnObservation struct {
	FinalText string
	// Outcome is one of the TurnOutcome constants.
	Outcome      string
	Reason       string
	SkillsLoaded []string
	// UserLabel is the session's display name for the user (magi.set_user_label,
	// e.g. an SSO plugin) — "" when unset. Lets an observer attribute captured
	// knowledge to the actual person.
	UserLabel string
}

type TurnObserver interface {
	UserMessage(sessionID, text string)
	TurnFinished(sessionID string, o TurnObservation)
}

// DefaultDangerTools is the set that needs asking before it runs, when Permission is "ask".
//
// A function rather than a literal buried in withDefaults because a tool registered OUTSIDE the
// built-in registry — one cmd wires in, like ask_companion — has to be able to add itself, and
// copying the whole list to add one entry is how the copy and the original drift. Every name here
// must be a tool this binary actually has; a literal left behind by a rename is a guardrail that
// silently covers one less thing (see TestPolicyToolNamesAreRealTools).
func DefaultDangerTools() map[string]bool {
	return map[string]bool{
		"write": true, "edit": true, "multiedit": true, "bash": true,
		"wait_for": true, "webfetch": true, "websearch": true, "port_owner": true,
	}
}

type Config struct {
	Model      session.ModelRef
	System     string
	MaxSteps   int
	Permission string // "allow" | "deny" | "ask" | "auto" (approval axis)
	// Interactive is true only when a human can answer permission prompts (the TUI).
	// Headless/automation leaves it false: a guardrail-forced prompt then resolves by
	// policy instead of blocking forever on a decision no one will give (which, with a
	// non-cancellable context, deadlocks the process). Immutable after construction.
	Interactive bool
	// AnswerWait bounds how long a prompt waits for a human before resolving by policy.
	//
	// Zero means wait forever, which is right when the human is sitting in front of the terminal
	// that is blocked. It is wrong for a daemon: the UI that would answer is a separate process
	// that may not be attached, may have been closed, or may be on a phone in another room. A
	// daemon that waits forever on an answer nobody is coming to give is a stopped agent that
	// looks like a working one.
	//
	// On expiry the prompt resolves the way a non-interactive run would — by policy — and says in
	// the log that nobody answered, so the transcript records a decision made by default rather
	// than one somebody made.
	AnswerWait time.Duration
	// DangerTools require permission before execution (when Permission == "ask").
	DangerTools map[string]bool

	// Guardrail policy (sandbox × approval, two-axis posture). Profile
	// is a posture preset (safe|standard|yolo) that fills the axes and rules
	// below when they're unset; explicit fields override the preset.
	Profile string // "safe" | "standard" | "yolo"
	Sandbox string // "read-only" | "workspace-write" | "full" (OS sandbox axis)
	// StoreDir is where the session logs live. It is not a setting anybody writes: the daemon
	// passes it so the sandbox can keep it read-only, because it sits under the user's cache
	// directory and that whole directory is writable so build tools keep working. Without it a
	// confined command can rewrite the record of what it just did.
	StoreDir string
	// Confine wraps an argv so the OS runs it under this machine's sandbox, or reports that there
	// is nothing to wrap with. Supplied by the binary that has the platform-specific code; nil
	// means unconfined, which is what a test gets and what a build with no sandbox has.
	//
	// It is here because the bash tool is not the only thing this process starts on somebody
	// else's say-so — a hook runs on every tool event, an MCP server runs for the daemon's whole
	// life — and those were spawned unconfined while bash beside them was not.
	Confine func(port.SandboxSpec, []string) ([]string, bool)
	// Allow/Deny are pattern rules "Tool(spec)" — e.g. Bash(git push:*),
	// Read(**/.env), WebFetch(domain:example.com). Deny is a hard floor; secret
	// paths are denied by default in addition to these.
	Allow []string
	Deny  []string
	// AllowDomains restricts WebFetch/bash network egress to these hosts (and
	// subdomains). Empty = no host allowlist.
	AllowDomains []string
	// MaxOutputTokens mirrors [limits] max_output_tokens: >0 means the provider caps each response
	// at the token level, which supersedes the reasoning-spin guard (a coarser byte-cap cancel), so
	// the guard defers to it. 0 = provider default → the spin guard stays the backstop.
	MaxOutputTokens int
	// ContextTokens mirrors [limits] context_tokens: >0 forces the context-window budget for
	// EVERY model, whatever the registry says. It is the operator's answer about this run, so it
	// is resolved at contextWindow() — the one seam the compaction trigger, the live meter and
	// /context all pass through — rather than baked into one registry entry at startup, which
	// left a model reached later by /route still on its seeded number.
	ContextTokens int

	// Agents carry per-agent model routing. Nothing spawns them — the roster is what /route edits.
	Agents map[string]AgentSpec
	// Bounded recursion limits (D7).
	// Concurrency was the bound on concurrently running subagents. NOTHING ENFORCES IT: the
	// semaphore it sized was allocated and never acquired, and the runaway counter beside it was
	// only ever reset. The field is kept so an existing config that sets it still loads, and
	// documented as inert rather than removed, because whether subagent concurrency should be
	// bounded is a decision to make deliberately — not one to take by deleting a knob.
	Concurrency int

	// TimeBudget, when > 0, is a USER-declared wall-clock target for a turn,
	// surfaced in the budget line as guidance (never a hard stop). Off by
	// default; keep it off for leaderboard/official comparison runs.
	TimeBudget time.Duration

	// Models supplies model metadata for cost, routing, and context-aware
	// compaction (M6). Defaulted if nil.
	Models *model.Registry
	// CompactRatio triggers auto-compaction when estimated context tokens exceed
	// ContextWindow * ratio. Defaulted to 0.8.
	CompactRatio float64
	// CompactKeep is the share of the compaction budget (window × CompactRatio) kept verbatim as
	// the recent tail when older history is folded. Defaulted to 0.25; outside (0,1) falls back.
	CompactKeep float64
	// CompactModel, when set, is the model the fold summaries are written with (same provider).
	CompactModel string

	// Experience is the shared team memory/skills store (D13). Optional.
	Experience port.ExperienceStore
	// Companion is this companion's declared name ([companion].name), when it has one. It stamps
	// shared-wiki edits' editor line: on a page every companion reads, "who wrote this" is the
	// question, and "agent" is not an answer.
	Companion string

	// Observer receives top-level conversation milestones (the plugin host's
	// user_message / turn_finished lifecycle events). Implementations must be
	// non-blocking — the host queues the event and returns. Optional.
	Observer TurnObserver

	// Hooks are lifecycle automations (PreToolUse/PostToolUse/Stop) that enforce
	// team procedure. Harness enables built-in defaults (e.g. gofmt on save).
	Hooks   []HookSpec
	Harness bool

	// Workflow drives substantial tasks through a deterministic, code-enforced
	// phase pipeline (localize → implement → verify → review → summarize) instead
	// of one free-form turn. VerifyCmd is the authoritative verification command
	// (auto-detected when empty); WorkflowMaxLoops bounds implement↔verify retries.
	Workflow         bool
	VerifyCmd        string
	WorkflowMaxLoops int

	// There is no AutoOrchestrate field any more. It promised a directive that forced the agent to
	// decompose and delegate to subagents past a context threshold — machinery that went out with
	// the delegation stack — and for a while the knob outlived the mechanism: a threshold nothing
	// read, defaulted at 0.6, documented as doing something. A field that advertises a behaviour
	// the application does not have teaches a reader something untrue, which is the same reason the
	// ToolEnv's dead Ask/Report fields went.

	// Providers are named LLM profiles (per-agent endpoint/key routing): an
	// AgentSpec.Provider names one of these to run on a different backend.
	Providers map[string]port.LLMProvider

	// Autocomplete carries the IDE-helper toggles and the two profile names (code / composer) the
	// thin completion calls route to. Templates carries the commit/PR house style injected into the
	// draft generators. Both are the config sections verbatim so the resolved On()/AmbientOn()
	// helpers and the profile names stay in one place; the daemon fills them, the web reader leaves
	// them zero (it relays every model call to the daemon and runs none itself).
	Autocomplete config.AutocompleteConfig
	Templates    config.TemplatesConfig

	// ProfileModels maps a profile name to its model, so the /route menu can
	// switch an agent to a profile (setting both provider and model) at runtime.
	ProfileModels map[string]string

	// ProfileDefs holds the full profile definitions (endpoint/key/model/headers)
	// so the /route menu can list and edit them. NewProvider builds a provider for
	// a profile added/edited at runtime.
	ProfileDefs map[string]ProfileDef
	NewProvider ProviderFactory

	// PermissionPersister, if set, persists "always allow (project)" decisions.
	PermissionPersister PermissionPersister
	// RoutePersister, if set, persists /route editor edits to config so they
	// survive restarts (best-effort; a write failure never blocks the edit).
	RoutePersister RoutePersister

	// Delegation is roster-gated, not a separate flag: an agent is offered as a
	// delegate/refine executor iff it is configured AND write-capable (producesFiles).
	// The default roster is read-only explorers only, so nothing is delegatable and
	// every delegate/refine step degrades to solo; DEFINING a write-capable agent in
	// [agents] is what turns delegation on for it. See delegatableAgents.

	// Council, when non-nil, gates loop termination at depth 0 (D14): when the
	// model would finish, a consensus council votes done/continue instead, and a
	// "continue" injects the members' aggregated feedback back into the loop. nil
	// disables the gate (the model's stop is final, the historical behavior).
	Council port.Council
	// CouncilRule is the consensus rule (default majority); CouncilMembers overrides
	// the default MAGI trio.
	CouncilRule    council.Rule
	CouncilMembers []council.Member
	// ContextWindowProber, when set, asks the LLM backend for a model's real context
	// window the first time an unseeded model is used (e.g. after a runtime /route
	// switch). Injected by the wiring layer (openai.Client.ProbeContextWindow) so the
	// app never imports an LLM adapter. nil = no probing; the registry default is used.
	ContextWindowProber func(context.Context, string) (int, bool)

	// BetweenTurns runs before a turn starts and never while one is running.
	//
	// It exists because the tool set is not fixed. Which companions are running here changes while
	// this process lives, and each of them is two tools; attached once at startup, a companion
	// that came up later could be handed work and not spoken to, and one that stopped left tools
	// in the list that could only fail.
	//
	// Doing that on a clock was refused, with a reason: a tool list that changes between one step
	// and the next lets a model call something that has just gone, and that failure is unreadable
	// afterwards. This is the answer to it — the one instant where no step is in flight, so the
	// set can change without anything being able to notice mid-thought.
	//
	// It runs on the turn's own context, before the first step, and again before each re-run. It
	// must be quick: the turn waits for it.
	BetweenTurns func(context.Context)
	// SubagentPrefs is the user's own settings per subagent, from config. An absent entry (or a
	// nil Enabled) means "no choice made", which falls back to what the tool declared — so a
	// subagent that ships off stays off until someone says otherwise.
	SubagentPrefs     map[string]SubagentPref
	SubagentPersister SubagentPersister
}

// withDefaults fills unset fields with sensible values.
func (c Config) withDefaults() Config {
	if c.MaxSteps == 0 {
		// 240: sized for Terminal-Bench 2.x-scale tasks (multi-deliverable builds,
		// proof/compile iteration), where 40 was the top cause of agent_timeout —
		// reval2 measured 15/19 timeouts as ceiling exhaustion mid-work. The ceiling
		// is a runaway backstop, not the pacing mechanism: the loop guard, stall
		// nudges, and the council gate stop unproductive turns long before it, and
		// the per-step budget line keeps the agent from treating it as a quota.
		c.MaxSteps = 240
	}
	c = c.applyProfile()
	if c.Permission == "" {
		c.Permission = "ask"
	}
	if c.DangerTools == nil {
		c.DangerTools = DefaultDangerTools()
	}
	if c.Concurrency == 0 {
		c.Concurrency = 8
	}
	if c.Models == nil {
		c.Models = model.NewRegistry()
	}
	// A ratio outside (0,1] cannot describe a share of the window, and taking one literally would
	// either compact on every step (<=0 makes the budget 0) or never (>1 puts it past the window,
	// where the provider's own overflow is the only remaining brake). Fall back to the default
	// rather than honor a number that has no reading.
	if c.CompactRatio <= 0 || c.CompactRatio > 1 {
		c.CompactRatio = 0.8
	}
	if c.CompactKeep <= 0 || c.CompactKeep >= 1 {
		c.CompactKeep = 0.25
	}
	if c.WorkflowMaxLoops == 0 {
		c.WorkflowMaxLoops = 3
	}
	return c
}

// applyProfile fills the sandbox/approval axes from the posture preset when they
// are unset. Explicit fields always win, so a profile is just a convenient
// default bundle (sandbox × approval, surfaced as safe/standard/yolo).
func (c Config) applyProfile() Config {
	switch c.Profile {
	case "safe":
		if c.Permission == "" {
			c.Permission = "ask"
		}
		if c.Sandbox == "" {
			c.Sandbox = "read-only"
		}
	case "yolo":
		if c.Permission == "" {
			c.Permission = "allow"
		}
		if c.Sandbox == "" {
			c.Sandbox = "full"
		}
	case "standard":
		// The recommended posture: auto-approve edits, prompt for commands and
		// network, confine writes to the workspace.
		if c.Permission == "" {
			c.Permission = "auto"
		}
		if c.Sandbox == "" {
			c.Sandbox = "workspace-write"
		}
	default:
		// No profile set: keep historical behavior — permission filled below, and
		// OS sandbox stays opt-in (empty Sandbox = unconfined) to avoid silently
		// cutting network/out-of-tree writes for existing users. The policy layer's
		// command scan + permission prompt still guard bash either way.
	}
	return c
}
