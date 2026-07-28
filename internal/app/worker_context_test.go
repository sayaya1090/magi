package app

import (
	"strings"
	"testing"
)

// TestWorkerContextIsOneListForEveryHandoff: the parent's context blocks were assembled at each
// call site, one `if x != "" { brief += x }` per block, and delegate and refine each kept their own
// list — which is how the mined contract came to reach one hand-off and not the other. The registry
// is the fix: both paths render THE SAME list, so a block added once is added everywhere.
func TestWorkerContextIsOneListForEveryHandoff(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.stateLocked(s.ID).meta = s
	a.stateLocked(s.ID).planConcern = "nothing captures the build output"
	a.mu.Unlock()
	a.storeSpecMine(s.ID, "⟨hard⟩ caml_shared_heap_sweep — the entry point named in the request")

	ctx := a.workerContext(s.ID)
	for _, want := range []string{"caml_shared_heap_sweep", "nothing captures the build output"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("worker context missing %q:\n%s", want, ctx)
		}
	}
	// Facts first, warnings last: a worker that stops reading early still has the identifiers it
	// must not invent.
	mi := strings.Index(ctx, "caml_shared_heap_sweep")
	ci := strings.Index(ctx, "nothing captures the build output")
	if !(mi < ci) {
		t.Errorf("block order must be specmine→concern:\n%s", ctx)
	}

	// The delegate hand-off appends the whole list after the brief, verbatim.
	brief := a.withWorkerContext(s.ID, "── YOUR PART\ndo the thing")
	if !strings.HasPrefix(brief, "── YOUR PART") || !strings.HasSuffix(brief, ctx) {
		t.Errorf("withWorkerContext must append the context AFTER the brief, unaltered:\n%s", brief)
	}
	// The refine hand-off renders the same string — differing only in its leading separator, which
	// is refineContext's job because it appends to a prompt rather than to a brief.
	if got := refineContext(ctx); got != "\n\n"+ctx {
		t.Errorf("refine must carry the identical block set:\n%s", got)
	}

	// A session with nothing to hand over adds nothing at all: a bare hand-off stays byte-identical.
	bare := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow", MaxAgents: 10})
	if c := bare.workerContext("nope"); c != "" {
		t.Errorf("no context must render as empty, got %q", c)
	}
	if b := bare.withWorkerContext("nope", "  brief  "); b != "brief" {
		t.Errorf("empty context must leave the brief untouched, got %q", b)
	}

	// Every registered block must be silent-when-empty and self-heading; the names exist so this
	// list can be read against the hand-offs in a diff.
	if len(workerContextBlocks) != 2 {
		t.Fatalf("registry changed to %d blocks — hold the new one against both hand-offs", len(workerContextBlocks))
	}
	for _, blk := range workerContextBlocks {
		if s := blk.render(bare, "nope"); strings.TrimSpace(s) != "" {
			t.Errorf("block %q must render empty when it has nothing to say, got %q", blk.name, s)
		}
	}
}

// TestSpecMineReachesTheWorker: the mined contract — the request's own identifiers, and the
// signatures a read-only pass actually found in the repository — was stored for the termination
// council and appended to the MAIN session. A delegate worker is a FRESH session whose whole
// context is the brief, and the curator that builds that brief never sees the mined note either,
// so the one block naming the identifiers the worker must not invent reached the worker on the
// solo path only.
func TestSpecMineReachesTheWorker(t *testing.T) {
	mined := "⟨hard⟩ caml_shared_heap_sweep — runtime/shared_heap.c:812\n⟨derived⟩ pool size 4096 bytes"
	got := specMineWorkerBrief(mined)
	if !strings.Contains(got, "caml_shared_heap_sweep") || !strings.Contains(got, "runtime/shared_heap.c:812") {
		t.Fatalf("the mined identifiers must survive verbatim:\n%s", got)
	}
	// Two clauses carry the whole point of the block: use a fixed name exactly, and do not treat a
	// derived reading as ground truth when the file itself disagrees.
	for _, want := range []string{"verbatim", "do not invent", "TRUST THE FILE"} {
		if !strings.Contains(got, want) {
			t.Errorf("the mined-contract block must say %q:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "── MINED CONTRACT") {
		t.Errorf("the block must head itself so it can be appended anywhere: %q", got[:24])
	}
	if specMineWorkerBrief("   ") != "" {
		t.Error("a blank mined note must add nothing")
	}
	// Flag off is the A/B baseline: byte-identical to no mining at all.
	t.Setenv("MAGI_WORKER_SPECMINE", "0")
	if specMineWorkerBrief(mined) != "" {
		t.Error("MAGI_WORKER_SPECMINE=0 must suppress the block entirely")
	}
}
