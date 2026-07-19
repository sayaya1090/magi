package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A CloneContext child's event log carries the parent's original user prompts
// (actors preserved), so a subagent's turnTask must come from the recorded spawn
// seed — the last-ActorUser fallback resolves to a stale parent request there.
func TestSeedPromptOf(t *testing.T) {
	a := &App{}
	sid := session.SessionID("s_seedtest")
	a.mu.Lock()
	a.stateLocked(sid).seedPrompt = "unit: fix the header math only"
	a.mu.Unlock()
	if got := a.seedPromptOf(sid); got != "unit: fix the header math only" {
		t.Fatalf("seedPromptOf = %q", got)
	}
	if got := a.seedPromptOf(session.SessionID("s_absent")); got != "" {
		t.Fatalf("absent session must be empty, got %q", got)
	}
}
