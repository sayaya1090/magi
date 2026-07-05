package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// progressFingerprint is CONTENT-addressed: it hashes {path → hash(current content)} over the
// files the agent changed, so it is stable across a rewrite of identical bytes or an
// edit-then-revert (the spin mutationEpoch and text-equality miss) and changes only when the
// bytes on disk actually change. These cases pin that contract.
func TestProgressFingerprint(t *testing.T) {
	t.Run("no change is empty", func(t *testing.T) {
		if fp := newRunGuard().progressFingerprint(); fp != "" {
			t.Fatalf("a turn that changed nothing has no fingerprint, got %q", fp)
		}
	})
	t.Run("rewriting identical bytes leaves it unchanged", func(t *testing.T) {
		g := newRunGuard()
		g.recordChange("a.txt", "", "v1")
		first := g.progressFingerprint()
		g.recordChange("a.txt", "IGNORED", "v1") // same after → same content on disk
		if g.progressFingerprint() != first {
			t.Fatal("re-writing the same bytes must not move the fingerprint")
		}
	})
	t.Run("edit then revert returns to the prior fingerprint", func(t *testing.T) {
		g := newRunGuard()
		g.recordChange("a.txt", "orig", "orig") // net no-op baseline
		base := g.progressFingerprint()
		g.recordChange("a.txt", "orig", "changed")
		if g.progressFingerprint() == base {
			t.Fatal("a real content change must move the fingerprint")
		}
		g.recordChange("a.txt", "orig", "orig") // revert
		if g.progressFingerprint() != base {
			t.Fatal("reverting to earlier content must restore the fingerprint (self-revert is not progress)")
		}
	})
	t.Run("different content differs and order is irrelevant", func(t *testing.T) {
		g1 := newRunGuard()
		g1.recordChange("a.txt", "", "A")
		g1.recordChange("b.txt", "", "B")
		g2 := newRunGuard()
		g2.recordChange("b.txt", "", "B") // reversed insertion order
		g2.recordChange("a.txt", "", "A")
		if g1.progressFingerprint() != g2.progressFingerprint() {
			t.Fatal("fingerprint must be order-independent (paths sorted)")
		}
		g2.recordChange("a.txt", "", "A-changed")
		if g1.progressFingerprint() == g2.progressFingerprint() {
			t.Fatal("changing one file's content must change the fingerprint")
		}
	})
}

func TestDirectionGateEnabledFlag(t *testing.T) {
	if !directionGateEnabled() {
		t.Fatal("direction gate is ON by default")
	}
	for _, v := range []string{"0", "off", "false", "no"} {
		t.Setenv("MAGI_DIRECTION_GATE", v)
		if directionGateEnabled() {
			t.Fatalf("MAGI_DIRECTION_GATE=%q must disable the gate", v)
		}
	}
}

// directionGateLLM: the main agent writes ONE deliverable, then keeps trying to finish with a
// DIFFERENT narration each time but NEVER changes the file again; the review-gate tester can
// never PASS. So after the first change the deliverable content is FROZEN while nothing ever
// verifies it — the varied-echo spin the direction-terminal gate exists to cut. Varied finish
// text ensures the idle-resubmission short-circuit (which needs byte-identical text) is not what
// fires, isolating D1.
type directionGateLLM struct {
	mu        sync.Mutex
	finishNum int
}

func (f *directionGateLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	prompt := promptText(r)
	var evs []port.ProviderEvent
	switch {
	case strings.Contains(prompt, "Independently VERIFY"): // tester: never confirms
		evs = []port.ProviderEvent{
			{Type: port.ProviderText, Text: "could not run it here.\nVERDICT: " + verdictBlocked},
			{Type: port.ProviderFinish},
		}
	case strings.Contains(prompt, "Independently REVIEW"): // reviewer: benign
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "the change looks fine."}, {Type: port.ProviderFinish}}
	default: // main agent
		if countWriteCalls(r) == 0 {
			evs = []port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_w", Name: "write", Args: []byte(`{"path":"out.txt","content":"v1"}`)}},
				{Type: port.ProviderFinish},
			}
		} else {
			f.finishNum++
			// Distinct narration each finish → the idle short-circuit (byte-identical text) never
			// fires, but the FILE is unchanged, so the fingerprint stays frozen.
			evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done, attempt " + string(rune('0'+f.finishNum))}, {Type: port.ProviderFinish}}
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

// alwaysContinueCouncil returns the D1 test's config: a council that NEVER approves (always
// Continue) plus the review-gate tester/reviewer agents. Without D1 such a council would spin
// to its round cap on a frozen deliverable; D1 lands it after one round.
func directionGateConfig() (*fakeCouncil, Config) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "not convinced — verify it actually works"},
	}}
	return fc, Config{
		Permission: "allow",
		ReviewGate: true,
		Council:    fc,
		Agents: map[string]AgentSpec{
			"tester":   {Name: "tester", Tools: []string{"read", "bash"}},
			"reviewer": {Name: "reviewer", Tools: []string{"read"}},
		},
	}
}

const d1Marker = "without further deliberation"

// With the direction gate ON, once the agent stops changing the deliverable (frozen
// fingerprint) and the tester still has not passed, the turn lands UNVERIFIED after a SINGLE
// council round instead of deliberating to the round cap — re-questioning an unchanged artifact
// cannot help. It never forces a pass: the finish is honestly labeled unverified.
func TestDirectionGateLandsUnverifiedOnFrozenDeliverable(t *testing.T) {
	fc, cfg := directionGateConfig()
	a, wd := newApp(t, &directionGateLLM{}, cfg)

	evs := submitAndDrain(t, a, wd)

	unv, ok := turnFinishedUnverified(t, evs)
	if !ok {
		t.Fatalf("expected exactly one TurnFinished; types %v", typesOf(evs))
	}
	if !unv {
		t.Error("a frozen, never-verified deliverable must land UNVERIFIED, not as a confident success")
	}
	if countType(evs, event.TypeError) != 0 {
		t.Errorf("the gate must land honestly, not error out; types %v", typesOf(evs))
	}
	if fc.calls != 1 {
		t.Errorf("D1 must cut deliberation after one round on a frozen deliverable, got %d council rounds", fc.calls)
	}
	if !hasDecidedNote(evs, d1Marker) {
		t.Errorf("the terminal landing must carry the direction-gate note (%q); decided notes: %v", d1Marker, decidedNotes(evs))
	}
}

// The same frozen-deliverable run with MAGI_DIRECTION_GATE=0 must NOT short-circuit: the council
// deliberates past one round (proving the behavior is gated behind the flag) and the D1 note is
// absent. The turn still lands honestly (the pre-existing gates arbitrate), just later.
func TestDirectionGateDisabledDoesNotShortCircuit(t *testing.T) {
	t.Setenv("MAGI_DIRECTION_GATE", "0")
	fc, cfg := directionGateConfig()
	a, wd := newApp(t, &directionGateLLM{}, cfg)

	evs := submitAndDrain(t, a, wd)

	if _, ok := turnFinishedUnverified(t, evs); !ok {
		t.Fatalf("expected a TurnFinished; types %v", typesOf(evs))
	}
	if fc.calls <= 1 {
		t.Errorf("with D1 disabled the frozen deliverable must deliberate past one round, got %d", fc.calls)
	}
	if hasDecidedNote(evs, d1Marker) {
		t.Errorf("with D1 disabled the direction-gate note must NOT appear; decided notes: %v", decidedNotes(evs))
	}
}

// decidedNotes reads the Note off every CouncilDecided event (hasDecidedNote lives in
// council_test.go). Used only for failure diagnostics here.
func decidedNotes(evs []event.Event) []string {
	var out []string
	for _, e := range evs {
		if e.Type == event.TypeCouncilDecided {
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) == nil {
				out = append(out, d.Note)
			}
		}
	}
	return out
}
