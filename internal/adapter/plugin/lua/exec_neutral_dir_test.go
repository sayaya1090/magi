package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A CLI used as a language model should not be standing in the user's workspace.
//
// magi.exec runs in the workspace, which is right for a plugin that acts on the project and wrong
// for one that shells out to a coding CLI only to borrow its model. `claude` walks up from its
// working directory for project configuration and puts what it finds in every request: measured in
// the magi repo, with every tool already denied, the same prompt cost 13,676 billed input tokens
// there and 2,094 in a directory outside it — 11,582 per call for a second copy of skills the shim
// had already written into the prompt itself.
//
// It is also the only isolation the antigravity shim has. claude takes a per-tool deny list and
// codex runs read-only, but agy is asked not to use its tools by the prompt alone, and a prompt is
// not an enforcement.
func TestNeutralDirRunsSomewhereWithNothingInIt(t *testing.T) {
	data, work := t.TempDir(), t.TempDir()
	// Something in the workspace worth not reading.
	if err := os.WriteFile(filepath.Join(work, "CLAUDE.md"), []byte("project doctrine"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logged strings.Builder
	h := NewHostWithConfig(HostConfig{
		Runtime: RuntimeInfo{Workdir: work},
		DataDir: data,
		Logf:    func(s string) { logged.WriteString(s + "\n") },
	})
	dir := writePlugin(t, `name="x"`+"\n"+`permissions=["exec:pwd"]`,
		`local a = magi.exec("pwd", {})
local b = magi.exec("pwd", {}, { neutral_dir = true })
magi.log("plain=" .. a.stdout:gsub("%s+$", ""))
magi.log("neutral=" .. b.stdout:gsub("%s+$", ""))`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// magi.log prefixes each line with the plugin name, so the marker is found inside the line.
	field := func(key string) string {
		for _, line := range strings.Split(logged.String(), "\n") {
			if i := strings.Index(line, key); i >= 0 {
				return strings.TrimSpace(line[i+len(key):])
			}
		}
		return ""
	}
	plain, neutral := field("plain="), field("neutral=")
	if plain == "" || neutral == "" {
		t.Fatalf("the plugin did not report both directories: %q", logged.String())
	}
	if plain == neutral {
		t.Fatalf("neutral_dir ran in the workspace anyway: %q", neutral)
	}
	// Named per plugin, so one backend's CLI cannot leave a file that lands in another's context.
	if !strings.HasSuffix(neutral, filepath.Join("neutral", "x")) {
		t.Errorf("neutral dir = %q, want it to end in neutral/x under the data dir", neutral)
	}
	entries, err := os.ReadDir(neutral)
	if err != nil {
		t.Fatalf("the neutral directory was not created: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("the neutral directory is not empty: %v", entries)
	}
}

// Without the option nothing changes: the workspace is still where a plugin's commands run, which
// is what every plugin that acts on the project depends on.
func TestExecStillRunsInTheWorkspaceByDefault(t *testing.T) {
	work := t.TempDir()
	var logged strings.Builder
	h := NewHostWithConfig(HostConfig{
		Runtime: RuntimeInfo{Workdir: work},
		DataDir: t.TempDir(),
		Logf:    func(s string) { logged.WriteString(s + "\n") },
	})
	dir := writePlugin(t, `name="x"`+"\n"+`permissions=["exec:pwd"]`,
		`local a = magi.exec("pwd", {})
local b = magi.exec("pwd", {}, { timeout = "5s" })
magi.log("a=" .. a.stdout:gsub("%s+$", "") .. " b=" .. b.stdout:gsub("%s+$", ""))`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// An opts table that says nothing about the directory must not move it — the option is opt-in,
	// and every other caller passes opts for the timeout alone.
	out := strings.TrimSpace(logged.String())
	i, j := strings.Index(out, "a="), strings.Index(out, " b=")
	if i < 0 || j < 0 {
		t.Fatalf("the plugin did not report both directories: %q", out)
	}
	a, b := out[i+2:j], strings.TrimSpace(out[j+3:])
	if a != b || !strings.HasSuffix(a, filepath.Base(work)) {
		t.Errorf("passing opts moved the working directory: a=%q b=%q", a, b)
	}
}
