// Package config loads magi configuration from TOML (D4). Config is optional;
// a missing file yields zero values and the CLI falls back to flags/env.
package config

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration (config.toml).
type Config struct {
	Model         string               `toml:"model"`
	BaseURL       string               `toml:"base_url"`
	Permission    string               `toml:"permission"`
	MCP           map[string]MCPServer `toml:"mcp"`            // name -> server
	Routing       map[string]string    `toml:"routing"`        // agent name -> model (M6 routing)
	ExperienceDir string               `toml:"experience_dir"` // shared experience store path (D13)
	Hooks         []Hook               `toml:"hooks"`          // lifecycle hooks (committable in .magi/config.toml)

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

	LLM           LLMConfig           `toml:"llm"`           // LLM backend tuning (custom headers, …)
	Orchestration OrchestrationConfig `toml:"orchestration"` // multi-agent behavior toggles
	Theme         ThemeConfig         `toml:"theme"`         // TUI color overrides (dark/light)
	Council       CouncilConfig       `toml:"council"`       // consensus termination gate (D14)

	// Plugins holds free-form per-plugin settings: [plugins.<name>] tables a
	// plugin reads via magi.config_get. The host passes each plugin only its
	// own section.
	Plugins map[string]map[string]any `toml:"plugins"`
}

// LLMConfig tunes the LLM backend connection. Headers are custom HTTP headers
// sent on every request to the backend/gateway (e.g. an in-house LiteLLM's
// X-CLIENT-API-KEY); values support ${ENV_VAR} expansion so secrets stay out of
// the committed file.
//
// Profiles are named backends an agent can be routed to via [routing] — each may
// have its own endpoint, key, model, and headers, so e.g. the planner runs on a
// cheap gateway and the coder on a strong one.
type LLMConfig struct {
	Headers  map[string]string     `toml:"headers"`
	Profiles map[string]LLMProfile `toml:"profiles"`
}

// OrchestrationConfig toggles multi-agent behaviors. Planner enables the
// pre-flight planner that decides whether to investigate solo or fan out to
// parallel read-only explorers; nil means default (on), set false to disable.
type OrchestrationConfig struct {
	Planner *bool `toml:"planner"`
}

// ThemeConfig overrides TUI colors per mode. Keys are Material Design 3 color
// roles: primary, accent, muted, outline, error, success, surface,
// primaryContainer, outlineVariant, warn. Any subset overrides the built-in
// NERV/MAGI defaults; unspecified roles keep their default value.
type ThemeConfig struct {
	Dark  map[string]string `toml:"dark"`
	Light map[string]string `toml:"light"`
}

// CouncilConfig configures the consensus termination gate (D14). When Enabled,
// the agent loop, instead of finishing when the model stops, convenes a council
// that votes done/continue; a "continue" injects the members' feedback and the
// loop keeps going. Disabled by default (it adds an LLM round at each would-be
// finish). Members default to the MAGI (Melchior/Balthasar/Casper) when empty.
type CouncilConfig struct {
	// Enabled toggles the consensus termination gate. nil = on by default; set
	// false to disable. (Pointer so "unset" is distinguishable from explicit false,
	// like orchestration.planner.)
	Enabled   *bool           `toml:"enabled"`
	Rule      string          `toml:"rule"`       // unanimous|majority|quorum:k|weighted:θ|veto:Name (default majority)
	MaxRounds int             `toml:"max_rounds"` // cap rounds per turn (default 3)
	Members   []CouncilMember `toml:"member"`     // [[council.member]] tables; empty = the MAGI
	// Verify is a shorthand for a single deterministic signal named "verify" the
	// council runs each round as evidence (D16). Signals adds more named checks
	// ([[council.signal]]). Both opt-in; the council judges on real test/build/lint
	// evidence, not just the agent's claim.
	Verify  string                `toml:"verify"`
	Signals []CouncilSignalConfig `toml:"signal"`
	// Criteria, when true, elicits explicit acceptance criteria from the task once
	// per turn (one extra LLM call) and gives them to the council as the contract,
	// so it judges "done" against concrete conditions. Opt-in (default off).
	Criteria bool `toml:"criteria"`
	// PlanAbsorb, when true, makes the plan-audit gate run one extra planner pass to
	// fold the council's non-blocking (warn/info) advice into the plan before execution.
	// Off by default: the advice is otherwise injected for the executor to heed without
	// the extra LLM call.
	PlanAbsorb bool `toml:"plan_absorb"`
}

// CouncilSignalConfig is a named deterministic check the council runs for evidence.
type CouncilSignalConfig struct {
	Name    string `toml:"name"`
	Command string `toml:"command"`
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

# --- Per-agent routing. A value naming an [llm.profiles.*] entry routes that
# agent to that backend (endpoint/key/model); any other value is a bare model on
# the default backend. ---
# [routing]
# explore = "fast"               # → [llm.profiles.fast] (different endpoint/key)
# coder   = "qwen3-coder:30b"    # bare model on the default backend

# Named backends: each agent routed to a profile runs on its own endpoint, key,
# model, and headers (e.g. planner/explore on a cheap gateway, coder on a strong
# one). ${ENV_VAR} is expanded. Omit base_url to reuse the default endpoint.
# [llm.profiles.fast]
# base_url = "https://fast.gateway/v1"
# api_key  = "${FAST_KEY}"
# model    = "gpt-oss:20b"
# [llm.profiles.fast.headers]
# X-CLIENT-API-KEY = "${FAST_CLIENT_KEY}"

# --- MCP servers (stdio or HTTP); tools auto-register ---
# [mcp.filesystem]
# command = "npx"
# args    = ["-y", "@modelcontextprotocol/server-filesystem", "."]
# [mcp.remote]
# url = "http://localhost:3000/mcp"
# [mcp.remote.headers]
# Authorization = "Bearer ${MCP_TOKEN}"

# --- Orchestration ---
# [orchestration]
# planner = true   # pre-flight planner: before a turn, decide solo vs parallel
#                  # read-only exploration. Default on; set false to disable.
#                  # Route it to a cheap backend with [routing] planner = "fast".

# --- Plugin settings: a [plugins.<name>] table is readable by that plugin via
# magi.config_get("key"). Plugins persist their own values with config_set. ---
# [plugins.my-plugin]
# endpoint = "https://config.corp.example/v1"

# --- Consensus council (D14): the loop's termination gate. ON BY DEFAULT — instead
# of finishing when the model stops, a council (the MAGI: Melchior/Balthasar/Casper)
# votes done/continue; a "continue" injects feedback and the loop keeps going. It
# adds an LLM round at each would-be finish; set enabled = false to turn it off. ---
# [council]
# enabled    = false        # the gate is on by default; uncomment to disable
# rule       = "majority"   # unanimous | majority | quorum:2 | weighted:0.6 | veto:Balthasar
# max_rounds = 3
# criteria   = true         # elicit explicit acceptance criteria (1 extra LLM call/turn) as the council's contract
# verify     = "go test ./..."   # opt-in: run each round, fed to the council as evidence
# [[council.signal]]             # more named checks (test/lint/typecheck), all fed as evidence
# name = "lint"
# command = "golangci-lint run"
# [[council.member]]        # omit members to use the MAGI defaults
# name = "Melchior"
# lens = "correctness"      # correctness | verification | completeness
# # model    = "qwen3-coder:30b"   # optional; defaults to the session model
# # provider = "fast"              # optional [llm.profiles.*] backend (mix cheap+strong)
# # weight   = 1

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
	var c Config
	path := filepath.Join(dir, "config.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	err = toml.Unmarshal(b, &c)
	return c, err
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
