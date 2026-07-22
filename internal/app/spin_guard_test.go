package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A single response streaming output past the cap with NO tool call is cancelled as a spin; one
// that emits a tool call (or stays under the cap) is not.
func TestReasoningSpinGuard(t *testing.T) {
	t.Setenv("MAGI_SPIN_CAP", "100")
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	actor := event.Actor{Kind: event.ActorAgent, ID: "x"}

	consume := func(evs []port.ProviderEvent) (streamStep, bool) {
		ch := make(chan port.ProviderEvent, len(evs)+1)
		for _, e := range evs {
			ch <- e
		}
		close(ch)
		cancelled := false
		res, err := a.consumeStream(context.Background(), session.SessionID("s_spin"), actor, ch,
			"m", "pt", "pr", func() { cancelled = true })
		if err != nil {
			t.Fatalf("consumeStream: %v", err)
		}
		return res, cancelled
	}

	// 6 × 31 bytes = 186 > 100, no tool call → spin, cancelled.
	var spinEvs []port.ProviderEvent
	for i := 0; i < 6; i++ {
		spinEvs = append(spinEvs, port.ProviderEvent{Type: port.ProviderReasoning, Text: "reasoning chunk of some length "})
	}
	if res, cancelled := consume(spinEvs); !res.reasoningSpun || !cancelled {
		t.Errorf("reasoning past cap without a tool call must spin+cancel (spun=%v cancelled=%v)", res.reasoningSpun, cancelled)
	}

	// Same volume but a tool call arrives first → not a spin.
	withTool := append([]port.ProviderEvent{{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c1", Name: "bash"}}}, spinEvs...)
	if res, cancelled := consume(withTool); res.reasoningSpun || cancelled {
		t.Errorf("a response that emitted a tool call must not spin (spun=%v cancelled=%v)", res.reasoningSpun, cancelled)
	}

	// Under the cap → not a spin.
	if res, _ := consume(spinEvs[:2]); res.reasoningSpun {
		t.Error("output under the cap must not spin")
	}
}

// When [limits] max_output_tokens is set, the provider caps each response at the token level, so
// the coarser spin guard defers to it and never fires.
func TestSpinGuardDefersToMaxOutput(t *testing.T) {
	t.Setenv("MAGI_SPIN_CAP", "100")
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow", MaxOutputTokens: 8000})
	ch := make(chan port.ProviderEvent, 10)
	for i := 0; i < 6; i++ {
		ch <- port.ProviderEvent{Type: port.ProviderReasoning, Text: "reasoning chunk of some length "}
	}
	close(ch)
	res, err := a.consumeStream(context.Background(), session.SessionID("s"),
		event.Actor{Kind: event.ActorAgent, ID: "x"}, ch, "m", "pt", "pr", func() {})
	if err != nil {
		t.Fatal(err)
	}
	if res.reasoningSpun {
		t.Error("with max_output_tokens set, the spin guard must defer (not fire)")
	}
}
