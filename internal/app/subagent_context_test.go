package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// Every per-item explorer is handed the overall goal; the scout's DISCOVERY explorer — the one
// that decides which items exist at all, and so what the whole fan-out will and will never look
// at — was handed only the planner's "discover" phrase. That phrase is written to be scoped by
// the task ("the source files in scope"), so without the task it is scoped by nothing.
func TestScoutDiscoveryExplorerIsOriented(t *testing.T) {
	got := scoutListPrompt("harden the retry path in the HTTP client", "the source files in scope")
	if !strings.Contains(got, "harden the retry path") {
		t.Errorf("the discovery explorer must be told what the list is FOR:\n%s", got)
	}
	if !strings.Contains(got, "List the source files in scope") {
		t.Errorf("the discover phrase must survive verbatim:\n%s", got)
	}
	// The output contract is what makes the reply parseable (parseList) — the goal must lead,
	// not displace it, and the "FIRST line must already be an item" rule must still be last so
	// it is not read as applying to the goal line.
	if i, j := strings.Index(got, "harden the retry"), strings.Index(got, "ONLY the items"); i > j {
		t.Errorf("the goal must LEAD the request, not trail the output contract:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "or closing remark.") {
		t.Errorf("the output contract must stay last:\n%s", got)
	}
	// Step context off (the A/B baseline) → byte-identical to the pre-orientation prompt.
	bare := scoutListPrompt("", "docs/*.md")
	if strings.HasPrefix(bare, "Overall goal") || !strings.HasPrefix(bare, "List docs/*.md.") {
		t.Errorf("no goal means no orientation line at all:\n%s", bare)
	}
	if scoutListPrompt("   ", "docs/*.md") != bare {
		t.Error("a blank goal must be treated as no goal")
	}
}

// A refine child is CLONED from the parent, which is not the same as being told: the acceptance
// checklist lives in the parent's stored checks (the child's own set is empty until its own audit
// fills it with checks for its own sub-plan), the ledger's exact paths stop arriving once the
// shared refine session stops re-cloning, and the council's unresolved concern is an ActorSystem
// message — which cloneConversation drops outright. runStepGate judges the step against all three.
func TestRefineChildGetsWhatTheCloneCannotCarry(t *testing.T) {
	checklist := workerChecklist([]council.DeliverableCheck{
		{Step: "2", Deliverable: "the log file", Source: "build.log", Assert: "non-empty"},
	}, 1)
	if checklist == "" {
		t.Fatal("precondition: step 2's check must render a checklist")
	}
	ledger := renderLedger([]ledgerEntry{{Step: "write the parser", Facts: "internal/parse/lex.go"}})
	concern := concernBrief("nothing captures the build output")

	got := refineContext(checklist, ledger, concern)
	for _, want := range []string{"Acceptance checklist", "build.log", "internal/parse/lex.go", "nothing captures the build output"} {
		if !strings.Contains(got, want) {
			t.Errorf("refine context missing %q:\n%s", want, got)
		}
	}
	// Order matches the delegate worker's, so the two hand-offs cannot drift into describing the
	// same step differently.
	ci, li, ni := strings.Index(got, "Acceptance checklist"), strings.Index(got, "internal/parse/lex.go"), strings.Index(got, "nothing captures")
	if !(ci < li && li < ni) {
		t.Errorf("blocks must keep the delegate order checklist→ledger→concern:\n%s", got)
	}
	// It appends to a prompt, so it must lead with its own separation and add nothing when
	// there is nothing to add — a bare refine hand-off must stay byte-identical.
	if !strings.HasPrefix(got, "\n\n") {
		t.Errorf("blocks must separate themselves from the prompt they follow: %q", got[:8])
	}
	if refineContext("", "   ", "") != "" {
		t.Error("nothing to say must append nothing")
	}
	if refineContext() != "" {
		t.Error("no blocks at all must append nothing")
	}
}
