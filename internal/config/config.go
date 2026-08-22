// Package config loads magi configuration from TOML (D4). Config is optional;
// a missing file yields zero values and the CLI falls back to flags/env.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration (config.toml).
// CompanionConfig declares this workspace's place in a team.
//
// Name is how another companion addresses it and Role is one line saying what it is for. Both are
// published in the daemon's record, so `magi --agents`, the console and the companions tool read
// the same declaration rather than three guesses at a directory name.
type CompanionConfig struct {
	Name string `toml:"name"`
	Role string `toml:"role"`
	// Team groups companions that do related work, and Hub marks the one that speaks for the team.
	//
	// This is addressing, not topology. Nothing routes through a hub, nothing is hidden behind one,
	// and every companion is still reachable by its own name: addressing a TEAM reaches its hub,
	// which then decides who in the team does the work — which is the point, because that decision
	// costs a reading of the team's roster and the hub is the one that should pay it repeatedly
	// rather than everybody else paying it once each.
	//
	// A team with no hub is still a team, and addressing it says so rather than picking a member.
	Team string `toml:"team"`
	Hub  bool   `toml:"hub"`
	// MCPPeers attaches every companion already running here as an MCP server, so this one can ask
	// them what they know without an operator writing a command per peer into a config file.
	//
	// Off unless asked for, because it is not free: two tools per peer in the tool list of every
	// prompt, and a subprocess per peer held open for the life of the daemon. A weak model given
	// thirty tools chooses worse among the ten that matter, and this tree has measured that.
	MCPPeers bool `toml:"mcp_peers"`
}

type Config struct {
	Model   string `toml:"model"`
	BaseURL string `toml:"base_url"`
	// EmbedModel names the model that turns text into vectors. It buys two things, and they are the
	// two halves of one question — what does this store already say. Reading: the semantic half of
	// searching what a companion wrote down, including the memories injected into every turn.
	// Writing: whether a memory or skill being proposed is one already held, which without it is
	// string equality and so lets every rephrasing land a second copy.
	// Empty means lexical search and textual identity, which is the default: an
	// embedding model is a second model to install and its endpoint is often not the chat one —
	// Anthropic has none at all and points at Voyage, and vLLM answers only when it is serving an
	// embedding model. MAGI_EMBED_BASE_URL and MAGI_EMBED_API_KEY point it elsewhere; unset, they
	// follow the chat backend, which is right for Ollama and for a LiteLLM proxy.
	EmbedModel string               `toml:"embed_model"`
	APIKey     string               `toml:"api_key"` // default backend key; ${ENV} expanded. Flag/env override (see cmd/magi)
	Permission string               `toml:"permission"`
	MCP        map[string]MCPServer `toml:"mcp"` // name -> server
	// Cron is the unattended work this workspace does on a schedule, name -> job. Only a daemon
	// runs them; an interactive session reads them so its editor can show them, and fires nothing.
	Cron          map[string]CronJob `toml:"cron"`
	ExperienceDir string             `toml:"experience_dir"` // shared experience store path (D13)
	Hooks         []Hook             `toml:"hooks"`          // lifecycle hooks (committable in .magi/config.toml)
	// Subagents records the user's own settings per plugin-declared subagent, written by
	// /subagents. Only entries a user actually touched are here — anything absent falls back to
	// what the tool declared, which is how a subagent can ship switched off and still be turned on
	// for good. A model set here overrides whatever the plugin asked to spawn with, so the choice
	// of a stronger (or cheaper) model for one specialist lives where the user can see it.
	Subagents map[string]SubagentConfig `toml:"subagents"`

	// Companion is what this workspace's magi calls itself within a team, and what it is for.
	// Belongs in the PROJECT file (.magi/config.toml) and is committable: whoever set the
	// workspace up is who knows what it is for, and the answer travels with the repo rather than
	// living in one laptop's global config where a second machine would have to be told again.
	Companion CompanionConfig `toml:"companion"`

	// Guardrail policy (two-axis posture). Profile is a posture preset
	// (safe|standard|yolo); Sandbox is the OS-confinement axis
	// (read-only|workspace-write|full). Allow/Deny are "Tool(spec)" pattern rules
	// (e.g. Bash(git push:*), Read(**/.env)); AllowDomains restricts network
	// egress. Secret paths are denied by default regardless.
	Profile      string   `toml:"profile"`
	Sandbox      string   `toml:"sandbox"`
	Allow        []string `toml:"allow"`
	Deny         []string `toml:"deny"`
	AllowDomains []string `toml:"allow_domains"`

	LLM      LLMConfig      `toml:"llm"`      // LLM backend tuning (custom headers, …)
	Theme    ThemeConfig    `toml:"theme"`    // TUI color overrides (dark/light)
	Council  CouncilConfig  `toml:"council"`  // consensus termination gate (D14)
	Limits   LimitsConfig   `toml:"limits"`   // token caps (per-request output, context budget)
	Sampling SamplingConfig `toml:"sampling"` // temperature / top_p / top_k sent with every request

	// Autocomplete configures the low-latency, no-council completion helpers: inline code
	// completion in the console editor, next-instruction suggestion in the composer, and the
	// file-being-edited ambient context. See AutocompleteConfig.
	Autocomplete AutocompleteConfig `toml:"autocomplete"`
	// Templates carries user-defined house style for the draft generators (commit message, PR
	// body) — the rules a person would otherwise re-type into the box every time.
	Templates TemplatesConfig `toml:"templates"`
	// Update governs automatic self-update for a daemon: whether it picks up a new release on its
	// own and restarts onto it. See UpdateConfig.
	Update UpdateConfig `toml:"update"`

	// Plugins holds free-form per-plugin settings: [plugins.<name>] tables a
	// plugin reads via magi.store_get. The host passes each plugin only its
	// own section.
	Plugins map[string]map[string]any `toml:"plugins"`
}

// LimitsConfig caps token usage — a safety valve against runaway generation on a weak model.
// MaxOutputTokens sets the per-request output cap sent to the LLM (0 = provider default).
// ContextTokens overrides each model's context-window budget used for compaction sizing
// (0 = use the model's probed/registered window).
// CompactRatio is the share of that window that may be used before auto-compaction runs
// (0 = the built-in default of 0.8; anything outside (0,1] falls back to it).
//
// The ratio decides a real trade, which is why it is settable rather than fixed: compacting
// EARLIER makes every later step cheaper to prefill (measured on fix-ocaml-gc, 2026-07-31: 91k
// tokens of accumulated context, 82% of it file reads, a median 86s per model call and 77 calls
// in three hours), while compacting LATER keeps more detail in front of the model and spends
// fewer summarization calls. Which one wins depends on the task and the window, so the number
// belongs to whoever is running it.
type LimitsConfig struct {
	MaxOutputTokens int     `toml:"max_output_tokens"`
	ContextTokens   int     `toml:"context_tokens"`
	CompactRatio    float64 `toml:"compact_ratio"`
}

// SamplingConfig sets the sampling parameters sent with every request ([sampling]):
//
//	[sampling]
//	temperature = 0.2
//	top_p       = 0.8
//
// Every field is a POINTER so "absent" and "zero" stay distinguishable: temperature 0 is a
// meaningful setting (greedy decoding), and a plain float would make it indistinguishable from
// not configuring one at all. An absent field leaves the provider's own default in force —
// which is the model's, not the server's: qwen3-coder-next ships temperature 1 / top_p 0.95 /
// top_k 40 in its Modelfile, and that is what a magi run used before this section existed.
//
// TopK is an extension, not part of the OpenAI schema, so it is sent only when set. Measured
// against Ollama's /v1 endpoint it is accepted and then ignored (the native /api/chat honors
// it), so treat it as backend-dependent.
//
// Deliberate per-call pins outrank this: the council polls its members at temperature 0 so a
// deliberation is reproducible, and configuring a temperature here does not un-pin them.
type SamplingConfig struct {
	Temperature *float64 `toml:"temperature"`
	TopP        *float64 `toml:"top_p"`
	TopK        *int     `toml:"top_k"`
	// ReasoningEffort is how much a thinking model is asked to think: "none" (Ollama honours it to
	// turn thinking off) or low|medium|high. Empty omits the field entirely, which is the only
	// safe default — a non-thinking model handed one can reject the whole request.
	//
	// It sits with the sampling knobs because that is what it is: something sent with every
	// request that shapes the answer rather than deciding where it goes. It was reachable only as
	// MAGI_REASONING_EFFORT, which is a machine-wide switch — and this is a per-companion choice
	// (a planner that should think, a formatter that should not).
	ReasoningEffort string `toml:"reasoning_effort"`
}

// LLMConfig tunes the LLM backend connection. Headers are custom HTTP headers
// sent on every request to the backend/gateway (e.g. an in-house LiteLLM's
// X-CLIENT-API-KEY); values support ${ENV_VAR} expansion so secrets stay out of
// the committed file.
//
// Profiles are named backends an agent can be routed to — a subagent pref, a
// council member, or the completion helpers name one — each with its own endpoint,
// key, model, and headers, so e.g. the planner runs on a cheap gateway and the
// coder on a strong one.
type LLMConfig struct {
	Headers  map[string]string     `toml:"headers"`
	Profiles map[string]LLMProfile `toml:"profiles"`
}

// ThemeConfig overrides TUI colors per mode. Keys are Material Design 3 color
// roles: primary, accent, muted, outline, error, success, surface,
// primaryContainer, outlineVariant, warn. Any subset overrides the built-in
// NERV/MAGI defaults; unspecified roles keep their default value.
type ThemeConfig struct {
	Dark  map[string]string `toml:"dark"`
	Light map[string]string `toml:"light"`
}

// CouncilConfig configures the council the agent calls through the `council` tool — for a reading on
// something it is unsure of, or to declare the task finished, which the members either accept or
// hand back with what is still undone. Members default to the MAGI (Melchior/Balthasar/Casper)
// when empty.
type CouncilConfig struct {
	// Enabled toggles the council. nil = on by default; set false to disable, which
	// also removes the `council` tool and with it the finish declaration. (Pointer so
	// "unset" is distinguishable from an explicit false.)
	Enabled *bool           `toml:"enabled"`
	Rule    string          `toml:"rule"`   // unanimous|majority|quorum:k|weighted:θ|veto:Name (default majority)
	Members []CouncilMember `toml:"member"` // [[council.member]] tables; empty = the MAGI
	// Preset trades reading depth for interactive latency: "full" (default) is the
	// 3-member MAGI; "light" is a single verification member — one cheap LLM call per
	// council call instead of three, for everyday chat-speed use. Explicit members
	// override the preset.
	Preset string `toml:"preset"` // "" | "full" | "light"
}

// AutocompleteConfig configures magi's IDE-style helpers. All three are model calls that fire on a
// pause in typing, so they must be cheap and cancellable — they run as thin one-shot completions on
// a routed profile, NOT as council-gated subagent turns (a keystroke cannot wait seconds for a
// finish vote). Each toggle is a POINTER so an absent key means ON: the default is that these help.
//
//   - Ambient  — the file the user has open in the console editor rides into the agent's per-turn
//     context, so a question about "this" is answered against the unsaved buffer, not stale disk.
//   - Code     — inline ghost-text completion inside the console file editor.
//   - Composer — next-instruction ghost text in the prompt composer (web + TUI).
//
// CodeProfile and ComposerProfile name an [llm.profiles.*] backend (see LLMConfig): the two are
// split on purpose — code completion wants a small fast fill-in-the-middle model, while composer
// suggestion reads the conversation and the user's phrasing and wants a slightly stronger one. A
// surface whose profile is unset self-disables rather than silently billing the main model on every
// keystroke. CrossSession lets composer suggestion mine this user's PAST sessions' prompts for their
// phrasing patterns — a read across the session boundary, hence its own switch.
type AutocompleteConfig struct {
	Enabled         *bool  `toml:"enabled"`          // nil = on (master switch for all three)
	Ambient         *bool  `toml:"ambient"`          // nil = on
	Code            *bool  `toml:"code"`             // nil = on
	Composer        *bool  `toml:"composer"`         // nil = on
	CodeProfile     string `toml:"code_profile"`     // [llm.profiles.*] name; empty = code completion off
	ComposerProfile string `toml:"composer_profile"` // [llm.profiles.*] name; empty = composer suggestion off
	CrossSession    *bool  `toml:"cross_session"`    // nil = on: learn from past sessions' prompts
}

func (a AutocompleteConfig) On() bool { return a.Enabled == nil || *a.Enabled }

// AmbientOn / CodeOn / ComposerOn fold in the master switch: a surface is on only if it is not
// itself disabled AND autocomplete as a whole is on.
func (a AutocompleteConfig) AmbientOn() bool  { return a.On() && (a.Ambient == nil || *a.Ambient) }
func (a AutocompleteConfig) CodeOn() bool     { return a.On() && (a.Code == nil || *a.Code) }
func (a AutocompleteConfig) ComposerOn() bool { return a.On() && (a.Composer == nil || *a.Composer) }

// CrossSessionOn reports whether composer suggestion may read past sessions. Default on, but
// meaningless unless ComposerOn — the caller checks both.
func (a AutocompleteConfig) CrossSessionOn() bool { return a.CrossSession == nil || *a.CrossSession }

// TemplatesConfig is the house style injected into the draft generators (DraftCommit, DraftPR).
// Empty leaves the generators exactly as they were — the templates are additive rules ("layer the
// commits", "no issue numbers", a required trailer), not a replacement for the base prompt.
type TemplatesConfig struct {
	Commit string `toml:"commit"`
	PR     string `toml:"pr"`
}

// UpdateConfig governs whether a daemon updates itself automatically. Auto defaults ON (nil = on):
// the operator chose automatic rollout, so a daemon picks up a new release on its own, verifies it,
// and restarts onto it. Turning it off leaves only the manual push from the console — which the
// toggle does NOT gate, since that is a deliberate, explicit action. The auto path never runs outside
// --daemon and honours --no-update-check, so a benchmark (which runs a headless one-shot, not a
// daemon) never auto-updates.
type UpdateConfig struct {
	Auto *bool `toml:"auto"` // nil = on
}

func (u UpdateConfig) AutoOn() bool { return u.Auto == nil || *u.Auto }

// SubagentConfig is one subagent's user settings. Enabled is a POINTER: nil means "no choice
// made", which is a different thing from false and is what lets the tool's own default stand.
type SubagentConfig struct {
	Enabled  *TolerantBool `toml:"enabled"`
	Model    string        `toml:"model"`
	Provider string        `toml:"provider"`
}

// TolerantBool is a bool that also accepts the QUOTED forms of itself.
//
// /subagents wrote `enabled = "true"` for a while — SetKey quotes every value it writes, and a bool
// went through it. The result parses as TOML and then fails the whole file against a typed field,
// so a user who switched a subagent on could not start magi at all until they hand-edited the
// config. Reading the quoted form keeps those files opening; the writer no longer produces them.
type TolerantBool bool

func (b *TolerantBool) UnmarshalTOML(v any) error {
	switch t := v.(type) {
	case bool:
		*b = TolerantBool(t)
		return nil
	case string:
		p, err := strconv.ParseBool(strings.TrimSpace(t))
		if err != nil {
			return fmt.Errorf("expected a boolean, got %q", t)
		}
		*b = TolerantBool(p)
		return nil
	}
	return fmt.Errorf("expected a boolean, got %T", v)
}

// IsEnabled reports whether the council gate is on: by default yes, unless
// explicitly disabled with `[council] enabled = false`.
func (c CouncilConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// CouncilMember is one configured council seat: a theme-name label, a judging
// lens (correctness|verification|completeness), an optional model override, and
// an optional weight (default 1).
type CouncilMember struct {
	Name     string  `toml:"name"`
	Lens     string  `toml:"lens"`
	Model    string  `toml:"model"`
	Provider string  `toml:"provider"` // names an [llm.profiles.*] backend; empty = default
	Weight   float64 `toml:"weight"`
}

// LLMProfile is a named LLM backend: a distinct endpoint/key/model/headers an
// agent can be routed to. All string values support ${ENV_VAR} expansion.
type LLMProfile struct {
	BaseURL string            `toml:"base_url"`
	APIKey  string            `toml:"api_key"`
	Model   string            `toml:"model"`
	Headers map[string]string `toml:"headers"`
}

// Hook is a lifecycle automation: event (PreToolUse|PostToolUse|Stop),
// match (tool name or "*"), and a shell command.
type Hook struct {
	Event   string `toml:"event"`
	Match   string `toml:"match"`
	Command string `toml:"command"`
}

// MCPServer declares an MCP server connection. Either URL (for HTTP transport)
// or Command (for stdio transport) must be specified, but not both.
type MCPServer struct {
	// HTTP transport (Streamable HTTP)
	URL     string            `toml:"url"`     // e.g. "http://localhost:3000/mcp"
	Headers map[string]string `toml:"headers"` // Custom HTTP headers (supports ${ENV_VAR} expansion)

	// stdio transport
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
	Env     []string `toml:"env"`
}

// CronJob is one unattended task: when to run, and what to ask for.
//
// # Why a named table and not a list
//
// Written as [cron.nightly-audit] rather than [[cron]], so a job has a name that is stable across
// edits. Two things follow from that and both are the reason: the editors in the TUI and the web
// console can change one job with SetKey and remove one with RemoveSection — the primitives that
// already exist in edit.go — and a person reading a diff sees which job changed rather than that
// the third element of an array did. It is also the shape MCP servers already use, so there is one
// idea here and not two.
//
// # What it does not carry
//
// No workdir: the daemon that runs a job is the one holding this workspace, and a job that could
// name another directory would be a way to make a companion work somewhere nobody was watching.
// No "last run" either — that is derived from the sessions the job created (see SessionMeta.Origin)
// rather than written back here, because a second record of when something happened is a second
// record that can disagree with the first.
type CronJob struct {
	// Schedule is a crontab line ("0 3 * * *") or a shorthand ("@daily"), read in local time.
	// Parsed by internal/core/cron; an unparseable one is reported at startup and skipped, never
	// silently treated as "never".
	Schedule string `toml:"schedule"`
	// Prompt is what the companion is asked, verbatim, in a session of its own.
	Prompt string `toml:"prompt"`
	// Enabled defaults to true when the key is absent, which is why it is a pointer: a job written
	// into the file is meant to run, and a plain bool would make every hand-written job start
	// switched off. Off is something a person says explicitly.
	Enabled *bool `toml:"enabled"`
}

// On reports whether the job should run. Absent means yes.
func (j CronJob) On() bool { return j.Enabled == nil || *j.Enabled }

// MergeCron overlays a project's jobs onto the machine's, project winning on a shared name.
//
// Here rather than inline at the two call sites that need it. The daemon's overlay assembles a
// whole Config, and the schedule tool needs only this one map; written twice, the two agree the day
// they are written and disagree the first time one of them learns something. Neither input is
// modified — the caller may be holding a config it did not load.
func MergeCron(global, project map[string]CronJob) map[string]CronJob {
	if len(global) == 0 && len(project) == 0 {
		return nil
	}
	out := make(map[string]CronJob, len(global)+len(project))
	for k, v := range global {
		out[k] = v
	}
	for k, v := range project {
		out[k] = v
	}
	return out
}

// defaultConfigTemplate is the commented, self-documenting config.toml written
// on first run so users can see and edit the available settings. Defaults are
// shown commented out — uncomment and change what you need. Values that are also
// CLI flags follow precedence: flag > env > config > built-in default.
const defaultConfigTemplate = `# magi configuration. Everything here is optional — magi runs on sensible
# defaults if this file is empty. Uncomment and edit what you need.
# Docs: docs/EXTENDING.md and docs/MANUAL.md.

# --- LLM backend (also settable via --model/--base-url or MAGI_MODEL/MAGI_BASE_URL) ---
# Default is Ollama's free cloud tier — run 'ollama signin' once (no GPU needed).
# model    = "gpt-oss:120b-cloud"
# base_url = "http://localhost:11434/v1"   # any OpenAI-compatible endpoint
# For a fully local run instead: model = "qwen3-coder:30b" (after 'ollama pull').

# Custom headers sent on every LLM request, e.g. an in-house gateway key.
# ${ENV_VAR} is expanded at runtime so secrets stay out of this file.
# [llm.headers]
# X-CLIENT-API-KEY = "${LITELLM_CLIENT_KEY}"

# --- Permissions / guardrails ---
# permission = "ask"            # ask | auto | allow | deny
# profile    = "standard"       # safe | standard | yolo
# sandbox    = "workspace-write"
# allow         = ["Bash(git status:*)"]
# deny          = ["Read(**/.env)"]
# allow_domains = ["api.github.com"]

# Named backends: an agent routed to a profile — a subagent (/subagents), a
# council member, the completion helpers below — runs on its own endpoint, key,
# model, and headers (e.g. planner/explore on a cheap gateway, coder on a strong
# one). ${ENV_VAR} is expanded. Omit base_url to reuse the default endpoint.
# [llm.profiles.fast]
# base_url = "https://fast.gateway/v1"
# api_key  = "${FAST_KEY}"
# model    = "gpt-oss:20b"
# [llm.profiles.fast.headers]
# X-CLIENT-API-KEY = "${FAST_CLIENT_KEY}"

# --- MCP servers (stdio or HTTP); tools auto-register as mcp__<name>__<tool> ---
# [mcp.filesystem]
# command = "npx"
# args    = ["-y", "@modelcontextprotocol/server-filesystem", "."]
# [mcp.remote]
# url = "http://localhost:3000/mcp"
# [mcp.remote.headers]
# Authorization = "Bearer ${MCP_TOKEN}"

# --- Plugin settings: a [plugins.<name>] table is readable by that plugin via
# magi.store_get("key"). Plugins persist their own values with store_set. ---
# [plugins.my-plugin]
# endpoint = "https://config.corp.example/v1"

# --- The council (the MAGI: Melchior/Balthasar/Casper). ON BY DEFAULT. The agent
# reaches it through the council tool: to ask for a reading, or to declare the task
# finished — a declaration the members either accept, ending the turn, or hand back
# with what is still undone. Set enabled = false to remove the tool entirely. ---
# [council]
# enabled    = false        # the council is on by default; uncomment to disable
# rule       = "majority"   # unanimous | majority | quorum:2 | weighted:0.6 | veto:Balthasar
# [[council.member]]        # omit members to use the MAGI defaults
# name = "Melchior"
# lens = "correctness"      # correctness | verification | completeness
# # model    = "qwen3-coder:30b"   # optional; defaults to the session model
# # provider = "fast"              # optional [llm.profiles.*] backend (mix cheap+strong)
# # weight   = 1

# --- Sampling sent with every request. Omit a key to keep the provider's default —
# which is really the MODEL's: an Ollama model carries temperature/top_p/top_k in its
# own Modelfile, and that is what runs when this section is absent. Deliberate per-call
# pins outrank these (the council polls its members at temperature 0 for reproducibility,
# and setting a temperature here does not un-pin them). Env MAGI_TEMPERATURE /
# MAGI_TOP_P / MAGI_TOP_K override, which is the convenient lever for an A/B run. ---
# [sampling]
# temperature = 0.2
# top_p       = 0.8
# top_k       = 20          # not an OpenAI field; sent only when set, and Ollama's /v1 ignores it
# reasoning_effort = "high" # thinking budget: none|low|medium|high. Omitted entirely when unset,
#                           # which is the only safe default — a non-thinking model handed one can
#                           # reject the request. MAGI_REASONING_EFFORT still wins.

# --- Color theme (TUI). Override any Material Design 3 role per mode; an
# unspecified role keeps the built-in NERV/MAGI default. Roles: primary, accent,
# muted, outline, error, success, surface, primaryContainer, outlineVariant, warn,
# and the council members melchior, balthasar, casper (their verdict colors). ---
# [theme.dark]
# primary   = "#FF7A1A"
# accent    = "#5CD8E6"
# surface   = "#211B14"
# melchior  = "#FFB454"
# balthasar = "#5CD8E6"
# casper    = "#FF8A8A"
# [theme.light]
# primary = "#B45309"
# accent  = "#0E7490"
# surface = "#F5EEE3"

# --- Completion & suggestions (docs/MANUAL.md §Autocomplete). Each helper is one thin
# model call on a routed [llm.profiles.*] backend; a surface with no profile self-disables.
# [autocomplete]
# enabled          = true        # master switch for all three helpers (default on)
# ambient          = true        # the file open in the web editor rides into the agent's context
# code             = true        # inline code completion in the web editor (daemon-side gate)
# composer         = true        # composer next-instruction suggestion, web + TUI
# code_profile     = "fast"      # [llm.profiles.*] for code completion (unset = off)
# composer_profile = "balanced"  # [llm.profiles.*] for composer suggestion (unset = off)
# cross_session    = true        # learn phrasing from this workspace's past sessions

# --- House rules folded into the commit/PR draft generators. Additive; empty = unchanged.
# [templates]
# commit = "Layer the commits: docs, then core, then the outward change."
# pr     = "Fill the review checklist; name what a reviewer should look at first."

# --- Daemon self-update (docs/MANUAL.md §Version / self-update).
# [update]
# auto = true   # (default on) a daemon picks up new releases itself, verifies with rollback,
#               # and restarts once idle. Off = the console's manual button only. Never merged
#               # from a project config; a source build refuses to auto-update regardless.
`

// WriteDefaultIfMissing writes a commented default config.toml into dir if one
// does not already exist, so users have a discoverable, editable settings file.
// It creates dir as needed. A pre-existing file is left untouched.
func WriteDefaultIfMissing(dir string) error {
	path := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(path); err == nil {
		return nil // already exists — never overwrite the user's file
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfigTemplate), 0o644)
}

// Load reads config.toml from dir. A missing file is not an error.
func Load(dir string) (Config, error) {
	c, _, err := LoadWithUnknown(dir)
	return c, err
}

// LoadWithUnknown is Load plus the list of TOML keys present in the file but not
// matched by any Config field — i.e. likely typos ("profil", "modle"). Callers
// surface these as warnings; unknown keys are deliberately NOT an error, so a
// config written for a newer binary (with keys this build doesn't know) still
// loads. Free-form sections (plugins, theme, mcp) decode into maps, so
// their arbitrary keys are consumed and never reported here. A missing file
// yields zero values, no keys, and no error.
func LoadWithUnknown(dir string) (Config, []string, error) {
	var c Config
	path := filepath.Join(dir, "config.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil, nil
		}
		return c, nil, err
	}
	md, err := toml.Decode(string(b), &c)
	if err != nil {
		return c, nil, err
	}
	var unknown []string
	for _, k := range md.Undecoded() {
		unknown = append(unknown, k.String())
	}
	return c, unknown, nil
}

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ExpandEnv expands ${ENV_VAR} patterns in the string with environment variables.
// If the environment variable is not set, it is left as-is.
func ExpandEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name from ${VAR}
		varName := match[2 : len(match)-1]
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match // Keep original if env var not found
	})
}
