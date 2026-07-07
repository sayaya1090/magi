package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func hasSpec(specs []port.ToolSpec, name string) bool {
	for _, s := range specs {
		if s.Name == name {
			return true
		}
	}
	return false
}

// resolveconcern is the orchestrator's alone: it must appear in the orchestrator's tool
// specs and be withheld from every subagent (which lacks the whole-task view a reset needs).
func TestResolveConcernGatedToOrchestrator(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.ResolveConcern{})
	a := New(store, completingLLM{}, reg, bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	agent := AgentSpec{Name: "default"} // allow-all

	if !hasSpec(a.toolSpecs(agent, false), "resolveconcern") {
		t.Error("orchestrator should be offered resolveconcern")
	}
	if hasSpec(a.toolSpecs(agent, true), "resolveconcern") {
		t.Error("a subagent must NOT be offered resolveconcern")
	}
}

// The committed guarantee behind the guarded reset: an orchestrator resolve is a tombstone
// that clears advisory memory, but a still-true concern re-raised by the deterministic
// producer REOPENS the key — so a reset can never launder a fact away.
func TestOrchestratorResetSelfHealing(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	sid := session.SessionID("s")
	seedSession(t, a, sid)
	key := concernPremiseKey
	council := event.Actor{Kind: event.ActorSystem, ID: "council"}
	orch := event.Actor{Kind: event.ActorAgent, ID: "orchestrator"}

	_ = a.appendConcernRaised(ctx, sid, council, key, "self-check", "unverified-premise", "fail", "d", "")
	_ = a.appendConcernResolved(ctx, sid, orch, key, "orchestrator", "handled")
	if concernOpen(a, ctx, sid, key) {
		t.Fatal("after an orchestrator resolve the key should be closed (advisory memory cleared)")
	}
	// Still-true → deterministic producer re-raises next turn.
	_ = a.appendConcernRaised(ctx, sid, council, key, "self-check", "unverified-premise", "fail", "d2", "")
	if !concernOpen(a, ctx, sid, key) {
		t.Fatal("a still-true concern must REOPEN after an orchestrator reset")
	}
}

// concernOpen re-reads the durable log and folds it, mirroring how the gate observes
// the live ledger.
func concernOpen(a *App, ctx context.Context, sid session.SessionID, key string) bool {
	evs, _ := a.store.Read(ctx, sid, 0)
	return hasKey(sessionConcerns(evs), key)
}
