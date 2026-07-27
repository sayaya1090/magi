package app

import (
	"strings"
	"testing"
)

// The guard-stop path catches a report that was CUT OFF; a model that narrates its reasoning to
// completion passes every test there and lands under a header telling the worker to reuse it
// verbatim. Live specimen, ~1,400 characters of a 7,553-character brief.
func TestDeliberativeExplorationLosesItsAuthority(t *testing.T) {
	thought := "Let me trace through the logic more carefully to understand the bug.\n\n" +
		"Looking at lines 620-651 in `/app/ocaml/runtime/shared_heap.c`:\n\n" +
		"After merging, `last_free_block` is NOT updated. This means the merged block's header now has " +
		"an increased wosize.\n\n" +
		"Actually wait - looking more carefully: when blocks are merged, the merged block keeps the same address.\n\n" +
		"Let me check what happens after the merge..."
	if !deliberative(thought) {
		t.Fatal("a reply that announces its next step, reverses itself, and breaks off is a thought, not a finding")
	}
	note := specMineFindingsNote(thought)
	for _, gone := range []string{"Reuse a FIXED identifier or path from here verbatim",
		"so this one stands and the note's guess does not"} {
		if strings.Contains(note, gone) {
			t.Errorf("a speculation must not carry the findings header's authority: %q still present", gone)
		}
	}
	for _, want := range []string{"WORKING THROUGH", "LEAD TO CHECK", "the FILE decides"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note must say what the text actually is: %q missing", want)
		}
	}
	if !strings.Contains(note, thought) {
		t.Error("the content itself is kept — only the authority is dropped")
	}
}

// A real findings list keeps the header it earned, including the clause that makes it outrank the
// execution note's predictions about file contents.
func TestAFindingsListKeepsItsHeader(t *testing.T) {
	findings := "ocaml/runtime/shared_heap.c — `pool_sweep(struct caml_heap_state* local, pool** plist, " +
		"sizeclass sz, int release_to_global_pool)` at line 336\n" +
		"ocaml/runtime/shared_heap.h — declares `caml_sweep`; there is no `caml_fl_sweep`\n" +
		"HACKING.adoc — build is `./configure && make world opt`"
	if deliberative(findings) {
		t.Fatal("a path — fact list must not be demoted")
	}
	if !strings.Contains(specMineFindingsNote(findings), "Reuse a FIXED identifier or path from here verbatim") {
		t.Error("a genuine findings list keeps the verbatim-reuse instruction")
	}

	// One deliberative aside that RESOLVES is still a finding — both signals are required.
	mixed := "Let me look at the sweep code first.\nocaml/runtime/shared_heap.c — `pool_sweep` at line 336"
	if deliberative(mixed) {
		t.Error("a report that deliberates and then lands is a finding")
	}
	// Two moves that never land is the shape that matters.
	if !deliberative("Let me check the header.\nHmm, that is not it.\nLet me check the source...") {
		t.Error("two moves ending on another move is a thought")
	}
}
