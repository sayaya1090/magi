package app

import (
	"context"
	"testing"
)

// With the default flags (force-delegate + curate ON, MAGI_* unset), a solo plan step must be
// rewritten to a worker delegate and actually run there — the invariant the bench relied on but
// which showed no worker spawned. Uses a worker agent with nil Tools (allow-all ⇒ delegatable).
func TestForceDelegateConvertsSoloByDefault(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "created and verified"}, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 4,
		Agents: map[string]AgentSpec{"worker": {Name: "worker", System: "x"}},
	})
	if names := a.delegatableAgents(); len(names) == 0 {
		t.Fatalf("worker (allow-all) must be delegatable, got %v", names)
	}
	if !forceDelegateEnabled() {
		t.Fatal("force-delegate must default ON")
	}

	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.stateLocked(s.ID).meta = s
	a.mu.Unlock()

	steps := []planStep{{Title: "make calc", Strategy: "solo", Task: "create calc.py"}}
	a.registerPlanTodos(context.Background(), s.ID, steps)
	findings, delegated := a.executeSteps(context.Background(), s, "goal", steps, 0)
	if !delegated {
		t.Errorf("force-delegate (default ON) must convert the solo step to a worker delegate; delegated=%v findings=%q", delegated, findings)
	}
}
