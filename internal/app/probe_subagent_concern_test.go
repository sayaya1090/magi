package app

// PROBE (UNCOMMITTED, observation-only) for Phase 3 — cross-boundary concern loss.
//
// Pathology being proven, deterministically and WITHOUT a live model:
//
//	A leaf subagent runs at depth>0 and never convenes a council (loop.go depth==0 gate),
//	so it produces no structural signals of its own to the parent. injectSubagentResult
//	forwards only res.Text — the child's PROSE. If the child fell into a research dead-end
//	and raised an unverified-premise concern before finishing, that concern is folded into
//	the CHILD's ledger but the parent, seeing only the narrative, never learns the fact is
//	unproven. The parent council then finishes on the child's word.
//
//	bubbleSubagentConcerns closes this: folding the finished child's ledger and re-raising
//	its open concerns onto the parent (scoped to the child agent) makes them first-class
//	evidence for the parent council.
//
// storeApp / seedSession live in concern_bubble_test.go (committed).

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestProbeSubagentConcernCrossesBoundary(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	parent := session.SessionID("s_parent")
	child := session.SessionID("s_child")
	seedSession(t, a, parent)
	seedSession(t, a, child)

	// The child raised an unverified-premise concern, then finished with only prose.
	_ = a.appendConcernRaised(ctx, child, event.Actor{Kind: event.ActorSystem, ID: "council"},
		"self-check/unverified-premise", "self-check", "unverified-premise", "fail",
		"the enzyme site was never looked up", "")

	// OLD behavior: forward only the child's text into the parent.
	_ = a.appendPromptText(ctx, parent, event.Actor{Kind: event.ActorAgent, ID: "subagent:explorer"},
		"[subagent explorer result]\nDesigned the construct; the BsaI site is standard.")

	// The gap: the parent, having only the prose, carries NO structural concern…
	pevs, _ := a.store.Read(ctx, parent, 0)
	if got := sessionConcerns(pevs); len(got) != 0 {
		t.Fatalf("precondition: prose-only injection should leave the parent ledger empty, got %v", keysOf(got))
	}
	// …even though the child's own ledger plainly holds the unproven premise.
	cevs, _ := a.store.Read(ctx, child, 0)
	if !hasKey(sessionConcerns(cevs), "self-check/unverified-premise") {
		t.Fatal("precondition: the child ledger must hold the raised concern")
	}

	// FIX: bubble the child's ledger across the boundary → the parent now sees it.
	a.bubbleSubagentConcerns(ctx, parent, "explorer", child)
	pevs, _ = a.store.Read(ctx, parent, 0)
	if !hasKey(sessionConcerns(pevs), "subagent:explorer/self-check/unverified-premise") {
		t.Fatalf("bubble-up must carry the child's concern to the parent; got %v", keysOf(sessionConcerns(pevs)))
	}
	t.Log("PROVEN: prose-only injection loses the child's unverified-premise; bubble-up carries it to the parent council.")
}
