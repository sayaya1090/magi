package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// The convened fact is what the record has to answer "what did the members actually see" with —
// its own doc comment says it carries the evidence they were given. It carried the task, the
// agent's plan, the agent's claim and the diff, and dropped the one block that is neither asked
// for nor narrated: what the turn's TOOLS produced. Observed live (headless-terminal, 2026-07-31):
// a council convened right after an orchestrator nudge, and whether its evidence block had
// survived that injection was unanswerable from the log — the field did not exist.
func TestTheConvenedFactCarriesTheToolEvidence(t *testing.T) {
	d := event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior"}, Rule: "majority",
		Task:    "implement the interface",
		Report:  "Let me run a final comprehensive test",
		Actions: "- bash `timeout 10 python3 …` → ok: Results: 12 30 8",
		Changes: "### headless_terminal.py\n+import pty",
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"actions"`) {
		t.Fatalf("the evidence the members judged on must be in the record:\n%s", b)
	}
	var back event.CouncilConvenedData
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Actions != d.Actions {
		t.Errorf("round-trip lost the tool evidence: %q", back.Actions)
	}
	// Absent on a round that genuinely had none, rather than an empty key on every council event.
	b, _ = json.Marshal(event.CouncilConvenedData{Round: 1, Rule: "majority"})
	if strings.Contains(string(b), `"actions"`) {
		t.Errorf("no evidence is not the same as an empty one:\n%s", b)
	}
}

// The record's copy is bounded the same way the diff is: a council round is a fact in an append-only
// log, and eight tool results at their own per-item cap would put tens of kilobytes into every one.
func TestTheRecordedEvidenceIsCapped(t *testing.T) {
	huge := strings.Repeat("x", councilDiffCap*3)
	got := truncateForCouncil(huge, councilDiffCap)
	if len(got) > councilDiffCap+64 { // the helper appends its own short marker
		t.Errorf("recorded evidence must stay bounded: %d bytes", len(got))
	}
	if len(got) == len(huge) {
		t.Error("nothing was truncated")
	}
}
