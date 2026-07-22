package app

import (
	"strings"
	"testing"
)

// handoffFacts must pull the HANDOFF section (the last weighted section of a rendered worker report)
// verbatim, and return "" when the finding carries none.
func TestHandoffFacts(t *testing.T) {
	finding := "### make dump (delegated to worker)\n" + reportStatusPrefix + "DONE\nDownloaded the archive.\n\n" +
		"EVIDENCE: sha256 ok\n\nHANDOFF: archive at /app/data/dump_2026.tar.gz (untar into /app/work)"
	got := handoffFacts(finding)
	want := "archive at /app/data/dump_2026.tar.gz (untar into /app/work)"
	if got != want {
		t.Errorf("handoffFacts = %q; want %q", got, want)
	}
	if h := handoffFacts("### x (delegated to worker)\n" + reportStatusPrefix + "DONE\njust prose, no handoff"); h != "" {
		t.Errorf("handoffFacts with no HANDOFF section = %q; want empty", h)
	}
}

// renderLedger must emit a verbatim block naming each step and its produced paths, and nothing for
// an empty ledger.
func TestRenderLedger(t *testing.T) {
	if s := renderLedger(nil); s != "" {
		t.Errorf("renderLedger(nil) = %q; want empty", s)
	}
	block := renderLedger([]ledgerEntry{
		{Step: "fetch data", Facts: "/app/data/in.csv"},
		{Step: "build proto", Facts: "/app/kv-store_pb2.py, /app/kv-store_pb2_grpc.py"},
	})
	for _, want := range []string{"SHARED DELIVERABLES LEDGER", "fetch data", "/app/data/in.csv", "/app/kv-store_pb2_grpc.py"} {
		if !strings.Contains(block, want) {
			t.Errorf("renderLedger missing %q\n---\n%s", want, block)
		}
	}
}

// appendLedger accumulates on the session; sharedLedger resolves a child to its PARENT's ledger so
// every worker in a plan sees the same shared deliverables.
func TestSharedLedgerParentResolution(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow", MaxAgents: 10})
	parent := parentSession(t.TempDir())
	child := parentSession(t.TempDir())
	child.Parent = parent.ID
	a.mu.Lock()
	a.stateLocked(parent.ID).meta = parent
	a.stateLocked(child.ID).meta = child
	a.mu.Unlock()

	a.appendLedger(parent.ID, "step one", "/app/out.bin")
	a.appendLedger(parent.ID, "step two", "") // empty facts → dropped
	a.appendLedger(parent.ID, "step three", "/app/next.json")

	own := a.ledgerOf(parent.ID)
	if len(own) != 2 {
		t.Fatalf("ledgerOf(parent) = %d entries; want 2 (empty facts dropped)", len(own))
	}
	shared := a.sharedLedger(child.ID)
	if len(shared) != 2 || shared[0].Facts != "/app/out.bin" {
		t.Fatalf("sharedLedger(child) must resolve to the parent's ledger, got %+v", shared)
	}
	if rows := a.SharedLedger(child.ID); len(rows) != 2 || rows[1].Step != "step three" {
		t.Errorf("SharedLedger(child) = %+v; want the parent's 2 rows", rows)
	}
}
