package app

import (
	"context"
	"strings"
	"testing"
)

// The plan-based exploration is default ON; MAGI_SPECMINE_EXPLORE=0 restores the prompt-analysis-only flow.
func TestSpecMineExploreEnabledGate(t *testing.T) {
	t.Setenv("MAGI_SPECMINE_EXPLORE", "")
	if !specMineExploreEnabled() {
		t.Fatal("plan-based spec exploration must default ON")
	}
	t.Setenv("MAGI_SPECMINE_EXPLORE", "0")
	if specMineExploreEnabled() {
		t.Fatal("MAGI_SPECMINE_EXPLORE=0 must disable it")
	}
}

// A read-only exploration's findings are folded into the mined note the check-author and termination
// council read — grounding the plan in the REAL repository.
func TestExploreSpecMineInjectsFindings(t *testing.T) {
	t.Setenv("MAGI_SPECMINE_EXPLORE", "1")
	a := newOrchApp(t, &gateLLM{text: "/app/server.py — class Server(dict) already defined"},
		Config{Permission: "allow", MaxAgents: 10, MaxDepth: 4})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.stateLocked(s.ID).meta = s
	a.mu.Unlock()

	a.exploreSpecMine(context.Background(), s, "build a KV server", []planStep{{Title: "impl", Strategy: "solo"}}, 0)

	note := a.cachedSpecMine(s.ID)
	if !strings.Contains(note, "Repository findings") || !strings.Contains(note, "server.py") {
		t.Fatalf("exploration findings must be folded into the mined note, got:\n%s", note)
	}
}

// Every guard short-circuits with no injection: disabled flag, a delegated worker (depth>0), and an
// empty plan. (Its own repository-exploration spawn never runs in these cases.)
func TestExploreSpecMineNoOps(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "should not be used"}, Config{Permission: "allow", MaxAgents: 10, MaxDepth: 4})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.stateLocked(s.ID).meta = s
	a.mu.Unlock()
	ctx := context.Background()
	steps := []planStep{{Title: "impl", Strategy: "solo"}}

	t.Setenv("MAGI_SPECMINE_EXPLORE", "1")
	a.exploreSpecMine(ctx, s, "t", steps, 1) // depth>0 (worker)
	a.exploreSpecMine(ctx, s, "t", nil, 0)   // empty plan
	if got := a.cachedSpecMine(s.ID); got != "" {
		t.Errorf("depth>0 / empty-plan must not inject, got %q", got)
	}
	t.Setenv("MAGI_SPECMINE_EXPLORE", "0")
	a.exploreSpecMine(ctx, s, "t", steps, 0) // flag off
	if got := a.cachedSpecMine(s.ID); got != "" {
		t.Errorf("disabled flag must not inject, got %q", got)
	}
}
