package app

// PROBE (UNCOMMITTED, observation-only) for Phase 4 — the guarded-reset safety rail.
//
// The risk Phase 4 introduces: the orchestrator can now RESOLVE a concern. A naive design
// would let a weak (or gamed) orchestrator silence a still-true fabrication concern to
// force a finish — the reset becomes a laundering channel.
//
// The rail that prevents it: the ledger is a fold of the event log, recomputed every turn,
// and the deterministic producer (council gate's unverifiedLookup) re-raises a still-true
// concern on the very next turn. A resolve is only a tombstone; a later raise REOPENS the
// key. So an orchestrator resolve can clear stale advisory memory but CANNOT suppress a
// fact that remains true — it comes right back.
//
// storeApp / seedSession live in concern_bubble_test.go (committed).

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestProbeOrchestratorResetCannotLaunder(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	sid := session.SessionID("s")
	seedSession(t, a, sid)
	key := concernPremiseKey

	// Turn N: the council gate raises an unverified-premise concern (deterministic producer).
	_ = a.appendConcernRaised(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "council"},
		key, "self-check", "unverified-premise", "fail", "premise never verified", "")
	if !concernOpen(a, ctx, sid, key) {
		t.Fatal("precondition: concern should be open after the raise")
	}

	// The orchestrator retires it — WITHOUT actually verifying anything (a laundering attempt).
	if err := a.appendConcernResolved(ctx, sid, event.Actor{Kind: event.ActorAgent, ID: "orchestrator"},
		key, "orchestrator", "trust me"); err != nil {
		t.Fatal(err)
	}
	if concernOpen(a, ctx, sid, key) {
		t.Fatal("a resolve should close the key in the fold (advisory memory cleared)")
	}

	// Next turn: the premise is STILL unverified, so the deterministic producer re-raises it.
	// This is exactly what runCouncilGate does when unverifiedLookup still fires.
	_ = a.appendConcernRaised(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "council"},
		key, "self-check", "unverified-premise", "fail", "premise STILL never verified", "")

	if !concernOpen(a, ctx, sid, key) {
		t.Fatal("PATHOLOGY: an orchestrator reset laundered a still-true concern away — it must REOPEN")
	}
	t.Log("PROVEN: an orchestrator resolve is only a tombstone; a still-true concern re-raises and reopens next turn.")
}
