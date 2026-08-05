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
	APIKey        string               `toml:"api_key"` // default backend key; ${ENV} expanded. Flag/env override (see cmd/magi)
	Permission    string               `toml:"permission"`
	MCP           map[string]MCPServer `toml:"mcp"`            // name -> server
	Routing       map[string]string    `toml:"routing"`        // agent name -> model (M6 routing)
	ExperienceDir string               `toml:"experience_dir"` // shared experience store path (D13)
	Hooks         []Hook               `toml:"hooks"`          // lifecycle hooks (committable in .magi/config.toml)
	// DisabledSubagents names the plugin-declared subagents the user switched off in /subagents.
	// A subagent ships disabled by declaring itself here; nothing in magi writes the list except
	// that command.
	DisabledSubagents []string `toml:"disabled_subagents"`

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
	c, _, err := LoadWithUnknown(dir)
	return c, err
}

// LoadWithUnknown is Load plus the list of TOML keys present in the file but not
// matched by any Config field — i.e. likely typos ("profil", "modle"). Callers
// surface these as warnings; unknown keys are deliberately NOT an error, so a
// config written for a newer binary (with keys this build doesn't know) still
// loads. Free-form sections (plugins, theme, mcp, routing) decode into maps, so
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
