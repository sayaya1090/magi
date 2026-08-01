package app

import (
	"os"
	"strings"
	"testing"
)

// The contract comment on TurnObserver tells a plugin author which endings to expect. Nothing held
// it against the endings the code produces, and it drifted in BOTH directions: it listed `guard`,
// which nothing emits, and omitted `ungated`, which was FORTY PERCENT of turns over eighty bench
// sessions (2026-08-02). An observer reading it would have handled an impossible case and been
// surprised by the second most common one.
//
// So the list is held against the constants. A new ending cannot ship without a line in the
// contract, which is the closest Go gets to a variant the compiler makes you choose.
func TestTurnOutcomesAreDocumented(t *testing.T) {
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	// The contract block is the run of comment lines from the sentence that introduces it down to
	// the interface it documents. Located by text rather than by walking the AST: the block is a
	// comment, the question is about its words, and a doc-comment lookup picked up a neighbouring
	// group on the first attempt — a subtler way for this test to check nothing.
	const lead = "TurnFinished carries the turn's STRUCTURAL outcome"
	i := strings.Index(string(src), lead)
	j := strings.Index(string(src), "type TurnObserver")
	if i < 0 || j < i {
		t.Fatal("the contract block was not found, so this test verified nothing")
	}
	contract := string(src)[i:j]
	if !strings.Contains(contract, "verified") {
		t.Fatalf("the located block is not the contract:\n%s", contract)
	}
	if len(turnOutcomes) == 0 {
		t.Fatal("no outcomes to check")
	}
	for _, o := range turnOutcomes {
		// Named at the start of a contract line — "\tverified   — …" — not merely mentioned
		// somewhere in the prose, which any word would satisfy.
		if !strings.Contains(contract, "//\t"+o+" ") {
			t.Errorf("outcome %q is produced but not listed in the TurnObserver contract:\n%s", o, contract)
		}
	}
	// …and nothing is listed that no constant produces. A reader must not be told to handle an
	// ending the code cannot reach — the reason `guard` carries its own disclaimer instead of
	// being quietly deleted.
	for _, line := range strings.Split(contract, "\n") {
		name, _, found := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(line, "//"), "\t"), " ")
		if !found || !strings.Contains(line, "—") || !strings.HasPrefix(line, "//\t") {
			continue
		}
		known := false
		for _, o := range turnOutcomes {
			if o == name {
				known = true
			}
		}
		if !known && name != "" && !strings.Contains(name, ".") {
			t.Errorf("the contract lists %q, which no TurnOutcome constant produces", name)
		}
	}
}

// The one ending the switch reaches by falling through is `done`, and it must stay a real outcome
// rather than the bucket a new path lands in unnoticed. This pins the default so a future edit
// that adds a branch has to say which outcome it produces.
func TestTheDefaultOutcomeIsDone(t *testing.T) {
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `outcome, reason := OutcomeDone, ""`) {
		t.Error("the outcome switch no longer starts from OutcomeDone; check what a new path falls into")
	}
	if strings.Contains(string(src), `outcome, reason = "`) || strings.Contains(string(src), `outcome = "`) {
		t.Error("an outcome is assigned as a bare string, so it can bypass the documented set")
	}
}
