package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// evidenceGateLLM writes exactly ONE deliverable and thereafter always says "done",
// while the review-gate tester always returns BLOCKED — it can never PASS the current
// version. So reviewPassedEpoch never catches up to the mutation epoch: the exact
// false-victory shape the execution-evidence gate exists to catch, where the agent
// insists it is finished but no independent run ever confirmed the deliverable.
type evidenceGateLLM struct {
	mu          sync.Mutex
	testerCalls int
}

func (f *evidenceGateLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	prompt := promptText(r)
	var evs []port.ProviderEvent
	switch {
	case strings.Contains(prompt, "Independently VERIFY"): // tester: can never confirm it
		f.testerCalls++
		evs = []port.ProviderEvent{
			{Type: port.ProviderText, Text: "could not run it here.\nVERDICT: " + verdictBlocked},
			{Type: port.ProviderFinish},
		}
	case strings.Contains(prompt, "Independently REVIEW"): // a reviewer: benign
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "the slice looks fine."}, {Type: port.ProviderFinish}}
	default: // main agent: one write, then always insist "done" (never fixes, never re-runs)
		if countWriteCalls(r) == 0 {
			evs = []port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_w", Name: "write", Args: []byte(`{"path":"out.txt","content":"v1"}`)}},
				{Type: port.ProviderFinish},
			}
		} else {
			evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done"}, {Type: port.ProviderFinish}}
		}
	}
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func evidenceGateConfig() Config {
	return Config{
		Permission: "allow",
		ReviewGate: true,
		Agents: map[string]AgentSpec{
			"tester":   {Name: "tester", Tools: []string{"read", "bash"}},
			"reviewer": {Name: "reviewer", Tools: []string{"read"}},
		},
	}
}

// turnFinishedUnverified returns the UNVERIFIED label off the (single) TurnFinished
// event, plus whether a TurnFinished was found at all.
func turnFinishedUnverified(t *testing.T, evs []event.Event) (unverified, found bool) {
	t.Helper()
	for _, e := range evs {
		if e.Type == event.TypeTurnFinished {
			var d event.TurnFinishedData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				t.Fatalf("unmarshal TurnFinishedData: %v", err)
			}
			return d.Unverified, true
		}
	}
	return false, false
}

func countLoopPushes(evs []event.Event) int {
	n := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "loop" {
			n++
		}
	}
	return n
}

// A top-level turn that changed a deliverable which was never independently run to a
// passing result must not land as a confident success: the council may judge the work,
// but the execution-evidence gate labels the finish UNVERIFIED. Before accepting it, the
// gate pushes exactly once ("run it for real, or say plainly you couldn't"); the agent
// here refuses to, so the turn ends honestly labeled rather than laundered as done.
func TestEvidenceGateLandsUnverifiedWhenNeverRun(t *testing.T) {
	llm := &evidenceGateLLM{}
	a, wd := newApp(t, llm, evidenceGateConfig())

	evs := submitAndDrain(t, a, wd)

	unv, ok := turnFinishedUnverified(t, evs)
	if !ok {
		t.Fatalf("expected exactly one TurnFinished; types %v", typesOf(evs))
	}
	if !unv {
		t.Errorf("a finish with no passing run for the current version must be labeled UNVERIFIED")
	}
	if countType(evs, event.TypeError) != 0 {
		t.Errorf("the gate must land honestly (labeled finish), not error out; types %v", typesOf(evs))
	}
	if n := countLoopPushes(evs); n != 1 {
		t.Errorf("execution-evidence gate should push exactly once before landing, got %d", n)
	}
}

// With MAGI_EVIDENCE_GATE=0 the gate is a no-op: the same never-verified turn finishes
// on the legacy path — unlabeled, and with no execution push — proving the behavior is
// gated behind the flag (the A/B knob) and off cleanly restores the prior semantics.
func TestEvidenceGateDisabledFinishesUnlabeled(t *testing.T) {
	t.Setenv("MAGI_EVIDENCE_GATE", "0")
	llm := &evidenceGateLLM{}
	a, wd := newApp(t, llm, evidenceGateConfig())

	evs := submitAndDrain(t, a, wd)

	unv, ok := turnFinishedUnverified(t, evs)
	if !ok {
		t.Fatalf("expected exactly one TurnFinished; types %v", typesOf(evs))
	}
	if unv {
		t.Errorf("with the gate disabled the turn must finish unlabeled (legacy behavior)")
	}
	if n := countLoopPushes(evs); n != 0 {
		t.Errorf("a disabled gate must not push, got %d", n)
	}
}

// The gate must not mislabel a genuinely-verified finish: when the tester independently
// PASSes the CURRENT version (here after one FAIL→fix→PASS), the turn lands clean — no
// UNVERIFIED label and no execution push. Makes the no-false-positive guarantee explicit
// rather than leaving it implicit in TestFreshEvidenceGateBlocksUntilTesterPasses.
func TestEvidenceGateDoesNotMislabelVerifiedFinish(t *testing.T) {
	llm := &freshEvidenceLLM{verdicts: []string{verdictFail, verdictPass}}
	a, wd := newApp(t, llm, evidenceGateConfig()) // gate ON (default)

	evs := submitAndDrain(t, a, wd)

	unv, ok := turnFinishedUnverified(t, evs)
	if !ok {
		t.Fatalf("expected exactly one TurnFinished; types %v", typesOf(evs))
	}
	if unv {
		t.Errorf("a turn the tester PASSed on the current version must NOT be labeled UNVERIFIED")
	}
	if n := countLoopPushes(evs); n != 0 {
		t.Errorf("a verified finish needs no execution push, got %d", n)
	}
}

// Every child hand-off contract must be symmetric with the pre-finish self-verify prompt:
// it may not ask "report how you verified it" without also licensing the honest negative
// (could-not-verify) and forbidding fabrication. Otherwise a weak model, given only the
// positive ask and no oracle, manufactures a verified-looking result to satisfy it.
func TestChildContractsCarryAntiFabricationClause(t *testing.T) {
	// A distinctive fragment of the single-sourced noFabricate clause.
	const marker = "never invent or hand-write output"
	cases := map[string]string{
		"delegate": delegatePrompt(planStep{Task: "do X"}, ""),
		"refine":   refinePrompt(planStep{Task: "do X"}, ""),
		"stuck":    stuckRedecomposePrompt("do X", "blocked on Y"),
	}
	for name, p := range cases {
		if !strings.Contains(p, marker) {
			t.Errorf("%s contract missing the anti-fabrication clause (%q):\n%s", name, marker, p)
		}
	}
}
