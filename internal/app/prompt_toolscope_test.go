package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

// The aggregation helpers appear ONLY in a bash-less agent's tool list: a bash-capable agent (e.g.
// the allow-all orchestrator) does the same with the shell, so carrying them is pure per-request
// weight, while a read-only explorer that lists them keeps them as its only way to reduce data.
func TestToolSpecsDataToolsBashScoped(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})

	names := func(agent AgentSpec) map[string]bool {
		m := map[string]bool{}
		for _, s := range a.toolSpecs(agent, false, 0) {
			m[s.Name] = true
		}
		return m
	}
	data := []string{"tabulate", "countmatches", "countlines", "groupby"}

	// Allow-all agent (nil Tools ⇒ has bash): data tools dropped.
	orch := names(AgentSpec{Name: "default"})
	if !orch["bash"] {
		t.Fatal("allow-all agent should carry bash")
	}
	for _, n := range data {
		if orch[n] {
			t.Errorf("bash-capable agent must NOT carry %q", n)
		}
	}

	// Read-only explorer (no bash, lists the data tools): keeps them.
	ro := names(AgentSpec{Name: "explore", Tools: append([]string{"read", "grep"}, data...)})
	if ro["bash"] {
		t.Fatal("read-only agent should not have bash")
	}
	for _, n := range data {
		if !ro[n] {
			t.Errorf("bash-less explorer must keep %q", n)
		}
	}
}
