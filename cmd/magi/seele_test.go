package main

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/plugins"
)

// Seele is the one agent magi bundles, and the whole point of bundling it is that it changes
// nothing until a user asks for it. These read the shipped files, because a manifest or a
// declaration that drifted would otherwise surface only on somebody's machine.
func TestSeeleShipsSwitchedOffAndCannotWrite(t *testing.T) {
	manifest, err := plugins.Embedded["seele"].FS.ReadFile("seele/plugin.toml")
	if err != nil {
		t.Fatalf("seele manifest: %v", err)
	}
	init, err := plugins.Embedded["seele"].FS.ReadFile("seele/init.lua")
	if err != nil {
		t.Fatalf("seele init.lua: %v", err)
	}
	man, lua := string(manifest), string(init)

	// The PLUGIN loads: a subagent nobody can find is not opt-in, it is hidden.
	if !plugins.Embedded["seele"].DefaultOn {
		t.Error("the plugin must load, or its subagent never appears in /subagents to be turned on")
	}
	// The SUBAGENT ships off. This is what keeps "magi provides no agent by default" true.
	if !strings.Contains(lua, "enabled = false") {
		t.Error("seele must declare enabled = false — it ships switched off")
	}
	if !strings.Contains(lua, "subagent = true") {
		t.Error("seele must declare itself a subagent, or it is not in the list a user manages")
	}

	// It cannot write, and not because it was asked nicely. Its child's allowlist carries read and
	// search only, and the host advertises nothing outside that list — so there is no write tool
	// for the model to reach for. Telling a weak model not to write is a different thing from
	// leaving it no way to.
	for _, banned := range []string{`"write"`, `"edit"`, `"multiedit"`, `"bash"`} {
		if strings.Contains(lua, "tools = {") && strings.Contains(spawnTools(lua), banned) {
			t.Errorf("seele's child allowlist includes %s — a planner must not be able to act", banned)
		}
	}
	for _, want := range []string{`"read"`, `"grep"`, `"glob"`, `"list"`} {
		if !strings.Contains(spawnTools(lua), want) {
			t.Errorf("seele's child allowlist is missing %s — it has to be able to read to plan", want)
		}
	}
	// No filesystem-write or exec permission in the manifest either, so nothing in the plugin's own
	// Lua can act on its behalf.
	for _, banned := range []string{"fs:write", "exec"} {
		if strings.Contains(man, banned) {
			t.Errorf("seele's manifest requests %q — a planner needs neither", banned)
		}
	}
	// And it declares the two capabilities it does need, so a load failure is not the first sign.
	for _, want := range []string{"tool", "spawn"} {
		if !strings.Contains(man, `"`+want+`"`) {
			t.Errorf("seele's manifest does not declare the %q capability", want)
		}
	}
}

// The planner is told it may decline. A plan for a one-line change costs more than the change, and
// an agent that always produces one manufactures work that was not asked for.
func TestSeeleMayDeclineToPlan(t *testing.T) {
	init, err := plugins.Embedded["seele"].FS.ReadFile("seele/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(init), "계획 불필요") {
		t.Error("seele's prompt must let it say a plan is unnecessary")
	}
}

// spawnTools returns the text of the tools={...} list in the spawn call, so a banned name elsewhere
// in the file (a comment, the prompt) is not mistaken for an allowlist entry.
func spawnTools(lua string) string {
	i := strings.Index(lua, "tools = {")
	if i < 0 {
		return ""
	}
	j := strings.Index(lua[i:], "}")
	if j < 0 {
		return lua[i:]
	}
	return lua[i : i+j]
}
