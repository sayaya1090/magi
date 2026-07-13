package main

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/plugins"
)

// The embedded engram plugin ships both files the loader needs, with the
// expected manifest identity — a broken go:embed path would otherwise only
// surface at a user's runtime.
func TestEmbeddedEngramComplete(t *testing.T) {
	manifest, err := plugins.Engram.ReadFile("engram/plugin.toml")
	if err != nil {
		t.Fatalf("embedded manifest missing: %v", err)
	}
	if !strings.Contains(string(manifest), `name = "engram"`) {
		t.Errorf("manifest lost its identity:\n%s", manifest)
	}
	code, err := plugins.Engram.ReadFile("engram/init.lua")
	if err != nil {
		t.Fatalf("embedded init.lua missing: %v", err)
	}
	for _, want := range []string{"magi.on(\"turn_finished\"", "magi.analyze", "register_context_provider"} {
		if !strings.Contains(string(code), want) {
			t.Errorf("embedded init.lua missing %q", want)
		}
	}
}
