package app

import (
	"context"
	"strings"
	"testing"
)

// A contract read short is the worst silent loss in the run: every later judgement is measured
// against it, so a criterion lost to a truncation is a condition nothing is ever asked to meet — and
// the short list is indistinguishable from a complete one. readCriteria is what separates them.
func TestReadCriteriaNamesWhatTheReadLost(t *testing.T) {
	whole := `{"criteria":["the server starts","Get returns the stored value"]}`
	if cs, damaged := readCriteria(whole); len(cs) != 2 || damaged != "" {
		t.Errorf("an intact reply must report no loss: %d criteria, damaged=%q", len(cs), damaged)
	}
	// Prose around it is not damage.
	if cs, damaged := readCriteria("Here is the contract:\n" + whole); len(cs) != 2 || damaged != "" {
		t.Errorf("prose around an intact object is not damage: %d criteria, damaged=%q", len(cs), damaged)
	}
	// Cut off mid-list: the second criterion is gone and the read has to say so.
	cs, damaged := readCriteria(`{"criteria":["the server starts","Get returns the stored value"`)
	if len(cs) != 1 || !strings.Contains(damaged, "DAMAGED") {
		t.Errorf("a truncated list must be reported: %d criteria, damaged=%q", len(cs), damaged)
	}
	// Whole list, one element that is not text: jsonx.Texts drops it (right — aborting would lose the
	// others), but the drop must not be invisible.
	cs, damaged = readCriteria(`{"criteria":["the server starts",{"c":"Get returns the value"},"it exits 0"]}`)
	if len(cs) != 2 {
		t.Fatalf("the readable criteria must survive the dropped element, got %v", cs)
	}
	if !strings.Contains(damaged, "3 entries") || !strings.Contains(damaged, "dropped") {
		t.Errorf("a dropped element must be reported: damaged=%q", damaged)
	}
	if cs, damaged := readCriteria("no object here"); cs != nil || damaged != "" {
		t.Errorf("prose carries nothing and lost nothing: %v %q", cs, damaged)
	}
}

// The draft re-asks once for the whole list and takes the repaired one.
func TestContractDraftReAsksWhenTheReplyWasDamaged(t *testing.T) {
	llm := &auditLLM{replies: []string{
		`{"criteria":["the server starts","Get returns the stored value"`,
		`{"criteria":["the server starts","Get returns the stored value","a missing key yields NOT_FOUND"]}`,
	}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)

	got := a.elicitContractDraft(context.Background(), AgentSpec{Name: "planner"}, s.ID, "m", "task")
	if len(got) != 3 {
		t.Fatalf("the repaired list must be used, got %v", got)
	}
	if n := len(llm.calls()); n != 2 {
		t.Fatalf("a damaged reply must cost exactly one re-ask (2 calls), got %d", n)
	}
	notes := sub.notes("contract-draft")
	if !strings.Contains(notes, "DAMAGED") || !strings.Contains(notes, "re-asking once") {
		t.Errorf("the damage must be reported before the re-ask:\n%s", notes)
	}
	// The reminder must name the defect and forbid the "helpful" shortening.
	retry := llm.calls()[1]
	for _, want := range []string{"COULD NOT BE READ IN FULL", "syntax error at offset", "Do NOT drop a criterion"} {
		if !strings.Contains(retry, want) {
			t.Errorf("the re-ask must carry %q, got:\n%s", want, retry)
		}
	}
}

// When the re-ask cannot repair it either, a partial contract still beats no contract — the council
// authors from scratch otherwise — but it must land as PARTIAL rather than as the whole thing.
func TestContractDraftLandsAPartialContractAsPartial(t *testing.T) {
	damaged := `{"criteria":["the server starts","Get returns the stored value"`
	llm := &auditLLM{replies: []string{damaged, damaged}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)

	got := a.elicitContractDraft(context.Background(), AgentSpec{Name: "planner"}, s.ID, "m", "task")
	if len(got) != 1 {
		t.Fatalf("the recovered criterion must be kept, got %v", got)
	}
	if n := sub.notes("contract-draft"); !strings.Contains(n, "PARTIAL contract") {
		t.Errorf("a partial contract must be named as one:\n%s", n)
	}
}

// Consolidation is ALLOWED to shorten the contract, so a short read here cannot be judged by length —
// only by whether the reply arrived intact. When it did not, the CURRENT contract is kept rather than
// replaced by whatever survived a broken reply.
func TestContractConsolidateKeepsTheCurrentContractOnADamagedReply(t *testing.T) {
	damaged := `{"criteria":["the server starts","Get returns the stored value"`
	llm := &auditLLM{replies: []string{damaged, damaged}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)

	cur := []string{"the server starts", "Get returns the stored value", "a missing key yields NOT_FOUND"}
	got, ok := a.consolidateContract(context.Background(), AgentSpec{Name: "planner"}, s.ID, "m", "task", cur, "fb")
	if ok || got != nil {
		t.Fatalf("a damaged consolidation must not replace the contract, got %v %v", got, ok)
	}
	n := sub.notes("contract-consolidate")
	if !strings.Contains(n, "reads exactly like applied feedback") {
		t.Errorf("the reason a short read cannot be trusted here must be stated:\n%s", n)
	}
	if !strings.Contains(n, "keeping the CURRENT contract") {
		t.Errorf("what the run proceeds with must be named:\n%s", n)
	}
}
