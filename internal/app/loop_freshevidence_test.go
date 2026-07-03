package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// freshEvidenceLLM plays three roles off one provider, branching on the prompt the
// loop (and spawn) hands it: the MAIN agent (writes a deliverable, then tries to
// finish), the review-gate TESTER (returns a scripted VERDICT), and a REVIEWER
// (benign note). It records how many times each ran so a test can prove the
// fresh-evidence gate re-verified after an edit rather than finishing on stale PASS.
type freshEvidenceLLM struct {
	mu          sync.Mutex
	testerCalls int
	writeCalls  int
	verdicts    []string // tester's verdict per call; the last repeats if over-called
}

// promptText flattens the request's system + message text so role/state can be
// detected from what the loop actually fed this call (tool results carry no text).
func promptText(r port.ChatRequest) string {
	var b strings.Builder
	b.WriteString(r.System)
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			b.WriteByte('\n')
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// countWriteCalls counts the main agent's write tool CALLS already in history, so the
// scripted agent knows how many deliverable versions it has produced.
func countWriteCalls(r port.ChatRequest) int {
	n := 0
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolCall && p.ToolCall != nil && p.ToolCall.Name == "write" {
				n++
			}
		}
	}
	return n
}

func (f *freshEvidenceLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	prompt := promptText(r)
	var evs []port.ProviderEvent
	switch {
	case strings.Contains(prompt, "Independently VERIFY"): // the tester
		v := f.verdicts[len(f.verdicts)-1]
		if f.testerCalls < len(f.verdicts) {
			v = f.verdicts[f.testerCalls]
		}
		f.testerCalls++
		evs = []port.ProviderEvent{
			{Type: port.ProviderText, Text: "ran the check.\nVERDICT: " + v},
			{Type: port.ProviderFinish},
		}
	case strings.Contains(prompt, "Independently REVIEW"): // a reviewer
		evs = []port.ProviderEvent{
			{Type: port.ProviderText, Text: "the slice looks correct and complete."},
			{Type: port.ProviderFinish},
		}
	default: // the main agent
		writes := countWriteCalls(r)
		sawFindings := strings.Contains(prompt, "Independent verification & review")
		switch {
		case writes == 0, sawFindings && writes < 2:
			// Produce (or, after a FAIL's findings, fix) the deliverable. Distinct content
			// each version so the guard treats the fix as a real mutation (epoch bump), not
			// an idempotent rewrite.
			f.writeCalls++
			id := "c_write_" + string(rune('0'+f.writeCalls))
			args := `{"path":"out.txt","content":"version ` + string(rune('0'+f.writeCalls)) + `"}`
			evs = []port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: id, Name: "write", Args: []byte(args)}},
				{Type: port.ProviderFinish},
			}
		default:
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

// The fresh-evidence gate must not let a deliverable-producing turn finish until the
// tester has independently PASSED the CURRENT version. A first FAIL blocks completion
// and forces a fix; the edit bumps the mutation epoch, so the prior verification is
// stale and the tester re-runs; only its PASS on the new version lets the turn finish.
// This proves the gate keys off real re-execution, not off the model asserting success.
func TestFreshEvidenceGateBlocksUntilTesterPasses(t *testing.T) {
	llm := &freshEvidenceLLM{verdicts: []string{verdictFail, verdictPass}}
	// Council disabled to isolate the review gate as the sole termination gate.
	a, wd := newApp(t, llm, Config{
		Permission: "allow",
		ReviewGate: true,
		Agents: map[string]AgentSpec{
			"tester":   {Name: "tester", Tools: []string{"read", "bash"}},
			"reviewer": {Name: "reviewer", Tools: []string{"read"}},
		},
	})

	evs := submitAndDrain(t, a, wd)

	if countType(evs, event.TypeTurnFinished) != 1 {
		t.Fatalf("turn should finish once the tester PASSes the fixed version; got types %v", typesOf(evs))
	}
	if countType(evs, event.TypeError) != 0 {
		t.Fatalf("no error event expected on a clean verified finish; got types %v", typesOf(evs))
	}
	llm.mu.Lock()
	testerCalls, writeCalls := llm.testerCalls, llm.writeCalls
	llm.mu.Unlock()
	// The first FAIL blocked the finish and forced a second deliverable version, which
	// the tester then re-verified — so both the tester and the writer ran exactly twice.
	if testerCalls != 2 {
		t.Errorf("tester should run twice (FAIL, then PASS on the fixed version), ran %d", testerCalls)
	}
	if writeCalls != 2 {
		t.Errorf("a blocked FAIL should force a second deliverable version; writes = %d, want 2", writeCalls)
	}
	// Each gate firing injects one review-findings prompt (actor "review").
	injected := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "review" {
			injected++
		}
	}
	if injected != 2 {
		t.Errorf("expected 2 review-findings injections (one per gate firing), got %d", injected)
	}
}
