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

// The recorded copy must keep BOTH ends. It used to drop the tail, and the tail of the evidence
// block is its most recent results — the ones a decision turns on. Observed in the field
// (cancel-async-tasks, 2026-07-31): the record ended `bash [error] pyth` and stopped, so the last
// thing the members were handed appeared, to anyone reading the record back, as a seventeen-
// character stub — and an earlier result looked like the final one.
func TestTheRecordedEvidenceKeepsBothEnds(t *testing.T) {
	const first, last = "── THE WORKSPACE RIGHT NOW ──", "tool bash [error] the last thing that happened: exit 1"
	s := first + strings.Repeat("\nfiller line that pads this out", 400) + "\n" + last

	got := clipEvidenceForRecord(s, councilDiffCap)
	if len(got) >= len(s) {
		t.Fatalf("nothing was clipped from %d bytes", len(s))
	}
	if !strings.HasPrefix(got, first) {
		t.Errorf("the head is gone:\n%.120s", got)
	}
	if !strings.HasSuffix(got, last) {
		t.Errorf("the most recent result is gone — the one a decision turns on:\n…%s", got[len(got)-120:])
	}
	if !strings.Contains(got, "bytes omitted from the middle") {
		t.Errorf("an unmarked cut reads as the whole record:\n%s", got)
	}
	// And it must not claim to be a diff: this block is not one, and the marker used to say so.
	if strings.Contains(got, "diff truncated") {
		t.Errorf("the actions block is not a diff:\n%s", got)
	}
}

// Short enough to fit is returned untouched — a marker on a complete record would say something was
// left out when nothing was.
func TestAnEvidenceRecordThatFitsIsUntouched(t *testing.T) {
	s := "── THE WORKSPACE RIGHT NOW ──\nrun.py — 1419 bytes"
	if got := clipEvidenceForRecord(s, councilDiffCap); got != s {
		t.Errorf("a record that fits was altered:\n%s", got)
	}
}
