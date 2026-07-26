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
// This file asserts CURRENT (buggy) behavior. After the ①+② fix lands, the derived
// signal (unverifiedLookup) must resurface the failure regardless of aging-out; the
// committed unit test TestUnverifiedLookup encodes that contract.

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

func TestProbeLookupFailAgesOut(t *testing.T) {
	evs := dnaLikeTurn()

	// Sanity: the failure IS in the full log (so a signal COULD be derived from it).
	full := ""
	for _, e := range evs {
		full += string(e.Data)
	}
	if !strings.Contains(full, "x509") {
		t.Fatal("probe setup wrong: failed lookup not in event log")
	}

	// PATHOLOGY: the council's evidence window omits the failed lookup entirely.
	window := turnToolEvidence(evs, councilActionsCap)
	t.Logf("council evidence window (last %d):\n%s", councilActionsCap, window)
	if strings.Contains(window, "websearch") || strings.Contains(window, "x509") {
		t.Fatalf("expected the failed lookup to have aged OUT of the council window, but it is still present:\n%s", window)
	}
	// And what the council DOES see is format-only success — nothing hinting the
	// premise was never verified.
	if !strings.Contains(window, "primers.fasta") {
		t.Fatalf("probe setup wrong: expected format-only actions in the window:\n%s", window)
	}
	t.Log("PROVEN: failed knowledge lookup is invisible to the council; only format-only success remains.")
}
