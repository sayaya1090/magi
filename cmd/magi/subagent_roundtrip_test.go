package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
)

// A subagent name is concatenated into a raw TOML table header, so it passes the same bare-key
// gate every other header writer applies (profiles, mcp, cron, schedule) — a name carrying a
// bracket or a newline is refused instead of rewriting the config file.
func TestPersistSubagentRefusesANonBareName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	p := subagentPersister{path: path}
	on := true
	for _, name := range []string{"a]\n[mcp.evil]", "a b", "a.b", ""} {
		if err := p.PersistSubagent(name, app.SubagentPref{Enabled: &on}); err == nil {
			t.Errorf("PersistSubagent(%q) wrote a header-breaking name", name)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a refused name still touched the config file")
	}
}

// Ticking a box in /subagents has to survive a restart, and that is the whole loop: the persister
// writes, the loader reads, and the app decides whether to advertise. Each half was covered on its
// own and the seam between them was not — so a bool written as a quoted string passed both halves
// and broke the config file, which then made the subagent look like it had never been turned on.
func TestASubagentChoiceSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	p := subagentPersister{path: path}

	on := true
	if err := p.PersistSubagent("planner_plan", app.SubagentPref{Enabled: &on, Model: "big-model"}); err != nil {
		t.Fatalf("persist: %v", err)
	}
	// Written as a REAL bool. The loader tolerates the quoted form so that files already on disk
	// still open, and that tolerance would otherwise hide a writer that went back to producing
	// them — so the bytes are asserted here, where the writer is.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "enabled = true") {
		t.Errorf("the config holds:\n%s\nwant an unquoted `enabled = true`", raw)
	}

	// The restart. A config that does not load is the failure this covers, so it is checked first
	// and loudly: everything after it would be vacuous.
	c, err := config.Load(dir)
	if err != nil {
		t.Fatalf("after switching a subagent on, the config no longer loads: %v", err)
	}
	prefs := toSubagentPrefs(c.Subagents)
	got, ok := prefs["planner_plan"]
	if !ok {
		t.Fatal("the subagent's settings did not come back")
	}
	if got.Enabled == nil || !*got.Enabled {
		t.Errorf("it came back switched %v, want on", got.Enabled)
	}
	if got.Model != "big-model" {
		t.Errorf("the model came back %q, want big-model", got.Model)
	}

	// And off again: the record has to carry both directions, or turning something off that ships
	// on would look like no choice at all.
	off := false
	if err := p.PersistSubagent("planner_plan", app.SubagentPref{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	c2, err := config.Load(dir)
	if err != nil {
		t.Fatalf("after switching it off, the config no longer loads: %v", err)
	}
	back := toSubagentPrefs(c2.Subagents)["planner_plan"]
	if back.Enabled == nil || *back.Enabled {
		t.Errorf("it came back %v, want off", back.Enabled)
	}
}

// A subagent nobody touched carries NO choice, so it keeps whatever the plugin declared. Collapsing
// an absent entry to false would switch off every subagent that ships on.
func TestAnUntouchedSubagentCarriesNoChoice(t *testing.T) {
	dir := t.TempDir()
	p := subagentPersister{path: filepath.Join(dir, "config.toml")}
	// Only a model, no enabled flag.
	if err := p.PersistSubagent("other", app.SubagentPref{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := toSubagentPrefs(c.Subagents)["other"]; got.Enabled != nil {
		t.Errorf("a subagent nobody switched carries a choice: %v", *got.Enabled)
	}
}
