package app

import (
	"context"
	"time"

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
}

// AgentRoute is an agent's current effective backend routing, for display and
// interactive editing (the /route menu).
type AgentRoute struct {
	Name     string
	Model    string
	Provider string // named profile, or "" for the default backend
}

// routeOverride is a runtime per-agent routing edit applied over config.
type routeOverride struct{ model, provider string }

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

// RoutePersister writes /route editor edits back to the config file so they
// persist across restarts. agent="" with a model persists the session default
// model; otherwise it persists [routing] agent = value (empty value clears).
type RoutePersister interface {
	PersistRoute(agent, value string) error
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
//	unverified — the turn landed but the council never approved (deadlock/cost/round cap)
//	guard      — a loop/stall guard force-stopped the turn
//	error      — the turn ended on an error event
//	done       — plain finish with no council verdict either way (e.g. conversational turn)
//
// Reason carries the unverified reason / guard code / error message ("" otherwise).
// SkillsLoaded lists skills the agent loaded (the skill tool) during the turn, so
// an observer can meter which skills actually get used and with what outcome.
type TurnObservation struct {
	FinalText    string
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
	// DangerTools require permission before execution (when Permission == "ask").
	DangerTools map[string]bool

	// Guardrail policy (sandbox × approval, two-axis posture). Profile
	// is a posture preset (safe|standard|yolo) that fills the axes and rules
	// below when they're unset; explicit fields override the preset.
	Profile string // "safe" | "standard" | "yolo"
	Sandbox string // "read-only" | "workspace-write" | "full" (OS sandbox axis)
	// Allow/Deny are pattern rules "Tool(spec)" — e.g. Bash(git push:*),
	// Read(**/.env), WebFetch(domain:example.com). Deny is a hard floor; secret
	// paths are denied by default in addition to these.
	Allow []string
	Deny  []string
	// AllowDomains restricts WebFetch/bash network egress to these hosts (and
	// subdomains). Empty = no host allowlist.
	AllowDomains []string

	// Agents are named subagents spawnable via the task tool.
	Agents map[string]AgentSpec
	// Bounded recursion limits (D7).
	MaxDepth    int // max subagent nesting depth
	MaxAgents   int // cumulative spawn cap (runaway backstop)
	Concurrency int // max concurrently running subagents
	// MaxPlanDepth caps how deep the pre-flight planner may recurse: a delegated
	// sub-task re-plans at its own level (heterogeneous recursion — each fanned-out
	// node independently picks solo/parallel/scout/delegate), but only while its
	// orchestration depth is below this. Kept TIGHTER than MaxDepth so a weak model's
	// plan can't explode into a deep decomposition tree; most work stays 1 level.
	MaxPlanDepth int

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

	// Experience is the shared team memory/skills store (D13). Optional.
	Experience port.ExperienceStore

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

	// Subagent supervision (sidecar): each background subagent is watched for
	// liveness and restarted on stall/timeout/transient error.
	SubagentTimeout     time.Duration // BASE hard cap per attempt (default 5m); the effective cap flexes with observed model speed (subagent_cap.go)
	SubagentStall       time.Duration // no-activity → considered stalled (default 4m), suppressed while a tool runs
	SubagentMaxRestarts int           // restarts on stall/timeout/error (default 2)

	// AutoOrchestrate triggers automatic orchestration mode when context usage
	// exceeds the threshold. -1 = disabled (user can toggle off), 0 = use default (0.6),
	// 0.0 < x <= 1.0 = custom threshold (e.g., 0.7 = 70%).
	// When triggered, the system injects a directive forcing the agent to
	// decompose work and delegate to subagents.
	AutoOrchestrate float64 // default 0.6 (60%)

	// Providers are named LLM profiles (per-agent endpoint/key routing): an
	// AgentSpec.Provider names one of these to run on a different backend.
	Providers map[string]port.LLMProvider

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

	// Planner enables the pre-flight planner: before a top-level turn it judges
	// whether the task splits into independent areas and, if so, fans out parallel
	// read-only explorers and injects their findings before the main agent runs.
	Planner bool

	// DisableDelegate turns off handing a write-capable sub-task to an executor
	// subagent (the delegate/refine strategies). When set, no agent is offered as a
	// delegate executor, so the planner uses only solo/parallel/scout and any
	// delegate/refine step degrades to solo — read-only explorers still fan out,
	// but all file-authoring work stays with the main agent. Negative polarity so the
	// zero value keeps delegation ENABLED, which the in-code tests rely on; the shipped
	// default is off — the config→app boundary maps [orchestration] delegate's
	// nil-default-off to this (nil or false ⇒ DisableDelegate true).
	DisableDelegate bool

	// Council, when non-nil, gates loop termination at depth 0 (D14): when the
	// model would finish, a consensus council votes done/continue instead, and a
	// "continue" injects the members' aggregated feedback back into the loop. nil
	// disables the gate (the model's stop is final, the historical behavior).
	Council port.Council
	// CouncilRule is the consensus rule (default majority); CouncilMaxRounds caps
	// council rounds per turn (default 3); CouncilMembers overrides the default
	// MAGI trio.
	CouncilRule      council.Rule
	CouncilMaxRounds int
	CouncilMembers   []council.Member
	// CouncilSignals are named deterministic checks run each council round and fed
	// to the members as evidence (D16) — so the council judges on proof (tests,
	// lint, typecheck) rather than the agent's claim. Opt-in (empty = no signals).
	CouncilSignals []CouncilSignalSpec
	// CouncilCriteria, when true, elicits explicit acceptance criteria once per
	// turn (one extra LLM call) and gives them to the council as its contract
	// (D15). Opt-in.
	CouncilCriteria bool
	// CouncilPlanAbsorb, when true, makes the plan-audit gate run one extra planner
	// pass to fold the council's non-blocking (warn/info) advice into the plan before
	// execution (Mechanism B). Off by default: the advice is otherwise injected for the
	// executor to heed without the extra LLM call.
	CouncilPlanAbsorb bool
	// ContextWindowProber, when set, asks the LLM backend for a model's real context
	// window the first time an unseeded model is used (e.g. after a runtime /route
	// switch). Injected by the wiring layer (openai.Client.ProbeContextWindow) so the
	// app never imports an LLM adapter. nil = no probing; the registry default is used.
	ContextWindowProber func(context.Context, string) (int, bool)
}

// CouncilSignalSpec is a named deterministic check the council runs for evidence
// (e.g. {Name:"test", Command:"go test ./..."}).
type CouncilSignalSpec struct {
	Name    string
	Command string
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
		c.DangerTools = map[string]bool{"write": true, "edit": true, "multiedit": true, "bash": true, "webfetch": true, "websearch": true}
	}
	if c.MaxDepth == 0 {
		c.MaxDepth = 3
	}
	if c.MaxAgents == 0 {
		c.MaxAgents = 50
	}
	if c.MaxPlanDepth == 0 {
		c.MaxPlanDepth = 2
	}
	if c.Concurrency == 0 {
		c.Concurrency = 8
	}
	if c.Models == nil {
		c.Models = model.NewRegistry()
	}
	if c.CompactRatio == 0 {
		c.CompactRatio = 0.8
	}
	if c.SubagentTimeout == 0 {
		// Tight per-attempt cap: a churning subagent (hallucinated targets, Q&A
		// ping-pong with the parent) stays event-active, so neither the stall
		// watchdog nor the tool-in-flight guard ever cuts it — the hard cap is the
		// only bound. At 30m one bad explorer outlived a whole bench task budget
		// (log-summary regression, 2026-07-10); 5m keeps a churner from eating the
		// parent's wall clock while legitimate focused subagent work fits well under.
		c.SubagentTimeout = 5 * time.Minute
	}
	if c.SubagentStall == 0 {
		// No-activity liveness: catches a truly wedged child (no events at all).
		// Stays ACTIVITY-based (any event, including streaming deltas) so a slow
		// single generation is not false-killed, and is suppressed entirely while a
		// tool is in flight (a silent long bash emits no events until it returns).
		c.SubagentStall = 4 * time.Minute
	}
	if c.SubagentMaxRestarts == 0 {
		c.SubagentMaxRestarts = 2
	}
	if c.WorkflowMaxLoops == 0 {
		c.WorkflowMaxLoops = 3
	}
	// AutoOrchestrate: 0 = use default, -1 = explicitly disabled
	if c.AutoOrchestrate == 0 {
		c.AutoOrchestrate = 0.6 // 60% context usage triggers auto-orchestration
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
