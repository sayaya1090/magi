package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// The file that decides what the agent may do is not one the agent may write.
//
// The project config lives inside the workspace, and the workspace is the tool jail — so `write`
// reached it. In a trusted workspace that file is taken as written, which means an agent could
// grant itself hooks, tool servers and an approval list; in `auto` mode the edit is approved
// without anybody seeing it. A plugin's manifest is the same shape one level down: it declares the
// permissions the host then grants it.
func TestAnAgentCannotWriteItsOwnGuardrails(t *testing.T) {
	p := newPolicy(nil, nil, nil)
	arg := func(path string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"path": path})
		return b
	}
	for _, path := range []string{
		".magi/config.toml",
		"deep/.magi/config.toml",
		".magi/plugins/helpful/plugin.toml",
		".magi/plugins/helpful/init.lua",
	} {
		for _, tool := range []string{"write", "edit", "multiedit"} {
			if v, _ := p.Decide(tool, arg(path)); v != "deny" {
				t.Errorf("%s(%s) → %q, wanted deny", tool, path, v)
			}
		}
		// Reading is fine: knowing your own posture is useful, and harmless.
		if v, _ := p.Decide("read", arg(path)); v == "deny" {
			t.Errorf("read(%s) is denied; only writing is the problem", path)
		}
	}
	// Ordinary files are untouched.
	if v, _ := p.Decide("write", arg("cmd/main.go")); v != "" {
		t.Errorf("write to a source file → %q", v)
	}
	// And a shell command naming one is flagged, which is the other half of the door: the file
	// tools are jailed, bash is not, so it forces the prompt instead.
	cmd, _ := json.Marshal(map[string]string{"command": "echo '[[hooks]]' >> .magi/config.toml"})
	v, why := p.Decide("bash", cmd)
	if v != "ask" || !strings.Contains(why, "protected path") {
		t.Errorf("a shell command writing the guardrail file → %q / %q", v, why)
	}
}
