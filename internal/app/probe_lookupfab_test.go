package app

// PROBE (UNCOMMITTED, observation-only) for N14 — fabrication-under-research-deadend.
//
// Pathology being proven, deterministically and WITHOUT a live model:
//
//	When a knowledge-lookup tool (websearch/webfetch) FAILS early in a turn and the
//	agent then produces a deliverable that rests on the un-looked-up fact, the failed
//	lookup ages out of the council's evidence window (turnToolEvidence keeps only the
//	last councilActionsCap=8 tool results). So the council never sees that a critical
//	premise was never verified — exactly the dna-assembly slip-through observed in the
//	o20-20 bench (BsaI=GATC hallucinated; real site GGTCTC).
//
// This file asserted the buggy behavior until 2026-09-05, when the window learned to keep
// the last result of every tool it would otherwise drop (council_evidence.go: the same
// aging-out hid a read_notes proof behind sixteen renders/reads on the IR deck). It now
// asserts the fixed shape: the failed lookup stays visible above the window, marked as kept.
// The derived signal (unverifiedLookup, TestUnverifiedLookup) remains the second line.

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// evPrompt / evToolCall / evToolResult live in council_lookup_test.go (committed).

// dnaLikeTurn reproduces the shape of the dna-assembly turn: a single user prompt,
// three failed knowledge lookups up front, then a run of successful format-only
// tool actions that build/inspect the deliverable.
func dnaLikeTurn() []event.Event {
	evs := []event.Event{evPrompt()}
	// Early: knowledge lookups all fail (x509 in the bench container).
	evs = append(evs,
		evToolCall("w1", "websearch"), evToolResult("w1", "search (duckduckgo) failed: x509: certificate signed by unknown authority", true),
		evToolCall("w2", "websearch"), evToolResult("w2", "search (duckduckgo) failed: x509: certificate signed by unknown authority", true),
		evToolCall("f1", "webfetch"), evToolResult("f1", "fetch failed: x509: certificate signed by unknown authority", true),
	)
	// Later: 8 successful format-only actions (design script, grep, echo lengths…),
	// enough to push the failed lookups past councilActionsCap.
	fmtActions := []struct{ name, out string }{
		{"write", "wrote design.py"},
		{"bash", "primers.fasta written"},
		{"bash", "grep >FWD primers.fasta -> >FWD_BsaI"},
		{"bash", "awk length -> 26"},
		{"bash", "Tm ~ 61.2 C"},
		{"write", "wrote primers.fasta"},
		{"read", "primers.fasta: 4 lines"},
		{"bash", "wc -l -> 4"},
	}
	for i, a := range fmtActions {
		id := "a" + string(rune('0'+i))
		evs = append(evs, evToolCall(id, a.name), evToolResult(id, a.out, false))
	}
	return evs
}

func TestProbeLookupFailStaysAboveWindow(t *testing.T) {
	evs := dnaLikeTurn()

	// Sanity: the failure IS in the full log (so a signal COULD be derived from it).
	full := ""
	for _, e := range evs {
		full += string(e.Data)
	}
	if !strings.Contains(full, "x509") {
		t.Fatal("probe setup wrong: failed lookup not in event log")
	}

	// FIXED: the window keeps the failed lookup above the last-k slice, marked as kept.
	window := turnToolEvidence(evs, councilActionsCap)
	t.Logf("council evidence window (last %d):\n%s", councilActionsCap, window)
	if !strings.Contains(window, "[kept from before the window — the last websearch result this turn] tool websearch [error]") ||
		!strings.Contains(window, "x509") {
		t.Fatalf("the failed lookup must stay visible above the window, marked as kept:\n%s", window)
	}
	// The window itself is still the format-only tail.
	if !strings.Contains(window, "primers.fasta") {
		t.Fatalf("probe setup wrong: expected format-only actions in the window:\n%s", window)
	}
}
