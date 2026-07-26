package app

// PROBE (UNCOMMITTED, observation-only) for Phase 2 — cross-turn concern survival.
//
// Pathology being proven, deterministically and WITHOUT a live model:
//
//	The N14 premise detector (unverifiedLookup) is judged per-turn: it fires only on
//	the turn a knowledge lookup fails, then resets at the next prompt boundary. But the
//	fact the deliverable rests on stays UNVERIFIED until it is actually looked up — so on
//	turn N+1 (the agent does unrelated format-only work, no lookup) the council that
//	convenes to FINISH the task sees nothing, even though the premise is still unproven.
//
//	The durable ledger closes this gap: a concern raised at the end of turn N stays open
//	across the prompt boundary, so sessionConcerns still surfaces it on turn N+1 — and,
//	crucially, mere ABSENCE of a lookup does NOT resolve it (only a real lookup success,
//	lookupRecovered, does). Absence must never launder a still-true concern away.
//
// evConcernRaised / hasKey / keysOf live in concern_test.go (committed);
// dnaLikeTurn / evPrompt / evToolCall / evToolResult live in the probe/lookup files.

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

func TestProbeConcernSurvivesAcrossTurns(t *testing.T) {
	// Turn N: failed lookups + format-only work, then the council raises a premise
	// concern into the ledger (what Phase 2 wiring does at the end of the turn).
	evs := dnaLikeTurn()
	evs = append(evs, evConcernRaised("self-check/unverified-premise", "unverified-premise", "BsaI site never verified", ""))

	// Turn N+1 boundary: a new prompt, only format-only work, NO knowledge lookup.
	evs = append(evs, event.Event{Type: event.TypePromptSubmitted},
		evToolCall("z1", "bash"), evToolResult("z1", "ran design.py -> ok", false))

	// Ephemeral per-turn detector is silent in N+1 (reset at the boundary) — this is the gap.
	if got := unverifiedLookup(evs); got != "" {
		t.Fatalf("per-turn detector should be silent in turn N+1, got %q", got)
	}
	// The ledger still carries the concern → the finishing council WILL see it.
	cs := sessionConcerns(evs)
	if !hasKey(cs, "self-check/unverified-premise") {
		t.Fatalf("ledger must carry the premise concern into turn N+1; got %v", keysOf(cs))
	}
	// Absence of a lookup is NOT recovery: the auto-resolve gate must stay shut.
	if lookupRecovered(evs) {
		t.Fatal("no lookup succeeded in N+1 — lookupRecovered must be false (absence != recovery)")
	}
	t.Log("PROVEN: only the durable ledger carries the unverified premise into turn N+1; absence does not resolve it.")
}
