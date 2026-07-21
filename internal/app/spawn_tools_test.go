package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A per-spawn SpawnRequest.Tools override (stored as curatedTools) scopes the child's tool list to
// exactly those tools — the plumbing the context curator uses to hand a worker a task-scoped set.
// nil override leaves the agent's own (here allow-all) list untouched.
func TestSpawnRequestToolsOverride(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"coder": {Name: "coder"}}, // nil Tools = allow-all
	})
	sid := session.SessionID("s_child")
	s := session.Session{ID: sid, Agent: "coder"}

	// No override → allow-all spec (nil Tools).
	if base := a.agentFor(s); len(base.Tools) != 0 {
		t.Fatalf("no override must leave allow-all (nil Tools), got %v", base.Tools)
	}

	// Override → the child's spec carries exactly the curated allowlist.
	a.mu.Lock()
	a.stateLocked(sid).curatedTools = []string{"read", "edit", "bash"}
	a.mu.Unlock()
	ov := a.agentFor(s)
	if len(ov.Tools) != 3 {
		t.Fatalf("override Tools = %v, want [read edit bash]", ov.Tools)
	}

	names := map[string]bool{}
	for _, sp := range a.toolSpecs(ov, true, 1) {
		names[sp.Name] = true
	}
	for _, n := range []string{"read", "edit", "bash"} {
		if !names[n] {
			t.Errorf("curated tool %q must be offered", n)
		}
	}
	if names["grep"] || names["lsp"] || names["write"] {
		t.Errorf("non-curated tool leaked into the worker's list: %v", names)
	}
}
