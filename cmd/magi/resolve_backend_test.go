package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/config"
)

// Three settings, four places each can come from, and the order matters in a way that has already
// been got wrong once: the config api_key was inert for the main backend until somebody noticed a
// key sitting in a file doing nothing.
//
// The shape to keep in mind is that the flag values arriving here have ALREADY absorbed env-or-
// builtin — the flag defaults do that — so the question is never "is the flag empty" but "did
// anything ahead of config speak".
func TestConfigFillsInOnlyWhenNothingAheadOfItSpoke(t *testing.T) {
	cfg := config.Config{Model: "cfg-model", BaseURL: "http://cfg", APIKey: "cfg-key"}
	none := func(string) string { return "" }

	// Nothing typed, nothing in the environment: config is what is left.
	got := resolveBackend(map[string]bool{}, backendFlags{model: "builtin", baseURL: "http://builtin"}, cfg, none)
	if got.model != "cfg-model" || got.baseURL != "http://cfg" || got.apiKey != "cfg-key" {
		t.Errorf("config did not fill in: %+v", got)
	}

	// Typed on the command line: config stays out of it, even though the flag value and the config
	// value are both non-empty.
	typed := map[string]bool{"model": true, "base-url": true, "api-key": true}
	got = resolveBackend(typed, backendFlags{model: "flag-model", baseURL: "http://flag", apiKey: "flag-key"}, cfg, none)
	if got.model != "flag-model" || got.baseURL != "http://flag" || got.apiKey != "flag-key" {
		t.Errorf("a typed flag lost to config: %+v", got)
	}

	// Set in the environment and NOT typed. The flag value already carries the env value, so the
	// test that matters is that config does not overwrite it — which a naive "flag is empty?" check
	// would get backwards.
	env := map[string]string{"MAGI_MODEL": "env-model", "MAGI_BASE_URL": "http://env", "MAGI_API_KEY": "env-key"}
	got = resolveBackend(map[string]bool{},
		backendFlags{model: "env-model", baseURL: "http://env", apiKey: "env-key"}, cfg,
		func(k string) string { return env[k] })
	if got.model != "env-model" || got.baseURL != "http://env" || got.apiKey != "env-key" {
		t.Errorf("config beat the environment: %+v", got)
	}
}

// OPENAI_API_KEY is what a machine already has set for everything else, and it counts as the
// environment having spoken just as ours does.
func TestEitherKeyEnvKeepsConfigOut(t *testing.T) {
	cfg := config.Config{APIKey: "cfg-key"}
	for _, name := range []string{"MAGI_API_KEY", "OPENAI_API_KEY"} {
		got := resolveBackend(map[string]bool{}, backendFlags{apiKey: "env-key"}, cfg,
			func(k string) string {
				if k == name {
					return "env-key"
				}
				return ""
			})
		if got.apiKey != "env-key" {
			t.Errorf("%s set and config still won: %q", name, got.apiKey)
		}
	}
}

// A key in a file is the form this tree tells people not to write; ${VAR} is the documented one,
// and without expansion it would arrive as the literal characters.
func TestAConfigKeyIsExpanded(t *testing.T) {
	t.Setenv("MAGI_TEST_SECRET", "sk-from-env")
	got := resolveBackend(map[string]bool{}, backendFlags{}, config.Config{APIKey: "${MAGI_TEST_SECRET}"},
		func(string) string { return "" })
	if got.apiKey != "sk-from-env" {
		t.Errorf("a ${VAR} in config was not expanded: %q", got.apiKey)
	}
}

// An empty config setting is not a setting. It must not blank out what the flag or the environment
// already resolved.
func TestAnEmptyConfigSettingChangesNothing(t *testing.T) {
	got := resolveBackend(map[string]bool{},
		backendFlags{model: "builtin", baseURL: "http://builtin", apiKey: "env-key"},
		config.Config{}, func(string) string { return "" })
	if got.model != "builtin" || got.baseURL != "http://builtin" || got.apiKey != "env-key" {
		t.Errorf("an empty config overwrote what was already resolved: %+v", got)
	}
}
