package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Switching a subagent on must not make the config unopenable.
//
// SetKey quotes every value it writes, which is right for a model name and silently wrong for a
// bool: `enabled = "true"` is valid TOML and then fails the WHOLE file against a typed field. A
// user who ticked a box in /subagents could not start magi at all until they hand-edited the file.
func TestTurningASubagentOnLeavesTheConfigLoadable(t *testing.T) {
	for _, on := range []bool{true, false} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		raw := "false"
		if on {
			raw = "true"
		}
		if err := SetRawKey(path, "subagents.planner_plan", "enabled", raw); err != nil {
			t.Fatal(err)
		}
		// Written as a bool, not as a quoted string.
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if want := "enabled = " + raw; !contains(string(b), want) {
			t.Errorf("config holds:\n%s\nwant a line %q", b, want)
		}

		c, err := Load(dir)
		if err != nil {
			t.Fatalf("a config with a subagent switched %v does not load: %v", on, err)
		}
		got := c.Subagents["planner_plan"].Enabled
		if got == nil {
			t.Fatal("the choice did not survive the round trip")
		}
		if bool(*got) != on {
			t.Errorf("enabled came back %v, want %v", bool(*got), on)
		}
	}
}

// A config ALREADY written by the version that quoted the bool must still open. Anyone who ticked
// the box before this fix has that file on disk, and refusing it means hand-editing TOML to start.
func TestAQuotedBoolFromTheOldWriterStillLoads(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[subagents.planner_plan]\nenabled = \"true\"\nmodel = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("a config written by the previous version does not load: %v", err)
	}
	got := c.Subagents["planner_plan"].Enabled
	if got == nil || !bool(*got) {
		t.Errorf("the quoted bool read as %v, want true", got)
	}
}

// Not tolerance of anything at all: a value that is neither a bool nor a spelling of one is still
// an error, so a typo is reported rather than read as false.
func TestANonBooleanEnabledIsStillAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[subagents.planner_plan]\nenabled = \"yeah\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("enabled = \"yeah\" was accepted")
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
