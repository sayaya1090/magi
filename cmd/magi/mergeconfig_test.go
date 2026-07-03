package main

import (
	"reflect"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
)

// mergeProjectConfig overlays a project .magi/config.toml onto the global config.
// The contract: scalars override only when the project set them; slices append;
// maps merge with the project value winning on a key collision. These tests pin
// each of those three behaviors so a future edit to the overlay can't silently
// flip precedence.

func TestMergeProjectConfig_ScalarsOverrideOnlyWhenSet(t *testing.T) {
	base := config.Config{
		Model:         "global-model",
		BaseURL:       "http://global",
		Permission:    "ask",
		Profile:       "safe",
		Sandbox:       "ro",
		ExperienceDir: "/global/exp",
	}

	// An all-empty project must leave every global scalar untouched.
	if got := mergeProjectConfig(base, config.Config{}); !reflect.DeepEqual(got, base) {
		t.Fatalf("empty project changed the config:\n got=%+v\nwant=%+v", got, base)
	}

	// A project that sets scalars overrides exactly those, leaving others intact.
	proj := config.Config{Model: "proj-model", Permission: "auto"}
	got := mergeProjectConfig(base, proj)
	if got.Model != "proj-model" {
		t.Errorf("Model: got %q, want proj-model", got.Model)
	}
	if got.Permission != "auto" {
		t.Errorf("Permission: got %q, want auto", got.Permission)
	}
	if got.BaseURL != "http://global" {
		t.Errorf("BaseURL should be untouched, got %q", got.BaseURL)
	}
	if got.Profile != "safe" || got.Sandbox != "ro" || got.ExperienceDir != "/global/exp" {
		t.Errorf("unset project scalars leaked: %+v", got)
	}
}

func TestMergeProjectConfig_SlicesAppend(t *testing.T) {
	base := config.Config{
		Allow:        []string{"g-allow"},
		Deny:         []string{"g-deny"},
		AllowDomains: []string{"g.example"},
		Hooks:        []config.Hook{{Event: "pre", Command: "g-hook"}},
	}
	proj := config.Config{
		Allow:        []string{"p-allow"},
		Deny:         []string{"p-deny"},
		AllowDomains: []string{"p.example"},
		Hooks:        []config.Hook{{Event: "post", Command: "p-hook"}},
	}
	got := mergeProjectConfig(base, proj)
	if want := []string{"g-allow", "p-allow"}; !eqStr(got.Allow, want) {
		t.Errorf("Allow: got %v, want %v", got.Allow, want)
	}
	if want := []string{"g-deny", "p-deny"}; !eqStr(got.Deny, want) {
		t.Errorf("Deny: got %v, want %v", got.Deny, want)
	}
	if want := []string{"g.example", "p.example"}; !eqStr(got.AllowDomains, want) {
		t.Errorf("AllowDomains: got %v, want %v", got.AllowDomains, want)
	}
	if len(got.Hooks) != 2 || got.Hooks[0].Command != "g-hook" || got.Hooks[1].Command != "p-hook" {
		t.Errorf("Hooks did not append in order: %+v", got.Hooks)
	}
}

func TestMergeProjectConfig_MapsMergeProjectWins(t *testing.T) {
	base := config.Config{
		Routing: map[string]string{"a": "g", "keep": "g"},
		LLM:     config.LLMConfig{Headers: map[string]string{"X": "g", "Keep": "g"}},
	}
	proj := config.Config{
		Routing: map[string]string{"a": "p", "add": "p"},
		LLM:     config.LLMConfig{Headers: map[string]string{"X": "p", "Add": "p"}},
	}
	got := mergeProjectConfig(base, proj)
	if got.Routing["a"] != "p" || got.Routing["keep"] != "g" || got.Routing["add"] != "p" {
		t.Errorf("Routing merge wrong: %v", got.Routing)
	}
	if got.LLM.Headers["X"] != "p" || got.LLM.Headers["Keep"] != "g" || got.LLM.Headers["Add"] != "p" {
		t.Errorf("LLM.Headers merge wrong: %v", got.LLM.Headers)
	}
}

func TestMergeProjectConfig_MapsMergeIntoNilBase(t *testing.T) {
	// A global config with no maps at all must still absorb project maps rather
	// than panic on a nil-map write.
	proj := config.Config{
		Routing: map[string]string{"a": "p"},
		MCP:     map[string]config.MCPServer{"m": {}},
		Plugins: map[string]map[string]any{"pl": {"k": 1}},
		LLM:     config.LLMConfig{Headers: map[string]string{"H": "p"}},
	}
	got := mergeProjectConfig(config.Config{}, proj)
	if got.Routing["a"] != "p" {
		t.Errorf("Routing not absorbed into nil base: %v", got.Routing)
	}
	if _, ok := got.MCP["m"]; !ok {
		t.Errorf("MCP not absorbed into nil base: %v", got.MCP)
	}
	if got.Plugins["pl"]["k"] != 1 {
		t.Errorf("Plugins not absorbed into nil base: %v", got.Plugins)
	}
	if got.LLM.Headers["H"] != "p" {
		t.Errorf("LLM.Headers not absorbed into nil base: %v", got.LLM.Headers)
	}
}

func TestMergeProjectConfig_Council(t *testing.T) {
	enabled := true
	base := config.Config{Council: config.CouncilConfig{
		Rule:      "majority",
		MaxRounds: 3,
		Signals:   []config.CouncilSignalConfig{{Name: "g"}},
	}}
	proj := config.Config{Council: config.CouncilConfig{
		Enabled:    &enabled,
		Rule:       "unanimous",
		MaxRounds:  5,
		Preset:     "light",
		Verify:     "go test ./...",
		Members:    []config.CouncilMember{{}, {}},
		Signals:    []config.CouncilSignalConfig{{Name: "p"}},
		Criteria:   true,
		PlanAbsorb: true,
	}}
	got := mergeProjectConfig(base, proj)
	c := got.Council
	if c.Enabled == nil || *c.Enabled != true {
		t.Errorf("Enabled not applied: %v", c.Enabled)
	}
	if c.Rule != "unanimous" || c.MaxRounds != 5 || c.Preset != "light" || c.Verify != "go test ./..." {
		t.Errorf("scalar council fields wrong: %+v", c)
	}
	if len(c.Members) != 2 {
		t.Errorf("Members should be replaced by project's 2, got %d", len(c.Members))
	}
	if len(c.Signals) != 2 || c.Signals[0].Name != "g" || c.Signals[1].Name != "p" {
		t.Errorf("Signals should append global+project: %+v", c.Signals)
	}
	if !c.Criteria || !c.PlanAbsorb {
		t.Errorf("Criteria/PlanAbsorb bools not OR'd in: %+v", c)
	}

	// An empty project council must not clobber global council settings.
	got2 := mergeProjectConfig(base, config.Config{})
	if got2.Council.Rule != "majority" || got2.Council.MaxRounds != 3 || got2.Council.Enabled != nil {
		t.Errorf("empty project council clobbered global: %+v", got2.Council)
	}
}

func eqStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
