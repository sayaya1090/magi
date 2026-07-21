package main

import "testing"

// With MAGI_WORKERS unset (the default), the roster must include a write-capable "worker" so the
// planner has a delegate target and force-delegate can route to it. This is the invariant the
// curated-worker architecture depends on; a bench run showed no worker spawned, so pin it.
func TestDefaultRosterHasWorker(t *testing.T) {
	agents := defaultAgents()
	w, ok := agents["worker"]
	if !ok {
		t.Fatal("worker must be in the DEFAULT roster (MAGI_WORKERS default ON)")
	}
	if len(w.Tools) != 0 {
		t.Errorf("worker should be allow-all (nil Tools ⇒ write-capable ⇒ delegatable), got %v", w.Tools)
	}
	// Explicitly disabled → absent.
	t.Setenv("MAGI_WORKERS", "0")
	if _, ok := defaultAgents()["worker"]; ok {
		t.Error("MAGI_WORKERS=0 must remove the worker")
	}
}
