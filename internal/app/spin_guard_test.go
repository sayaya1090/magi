package app

import (
	"context"
	"strings"
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

// The spin cap is on COMBINED text+reasoning output, not either channel alone: a response that
// interleaves text and reasoning, neither of which alone crosses the cap but whose SUM does, with
// no tool call, must still spin. Locks the reasoning.Len()+text.Len() semantics against a
// regression that checked only one channel (which would let a mixed spin slip through).
func TestSpinGuardSumsTextAndReasoning(t *testing.T) {
	t.Setenv("MAGI_SPIN_CAP", "100")
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ch := make(chan port.ProviderEvent, 6)
	chunk := strings.Repeat("x", 30)
	// T,R,T,R = 4×30 = 120 > 100, but at the crossing point text=60 and reasoning=60, so
	// neither channel alone exceeds the cap — only the sum does.
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: chunk}
	ch <- port.ProviderEvent{Type: port.ProviderReasoning, Text: chunk}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: chunk}
	ch <- port.ProviderEvent{Type: port.ProviderReasoning, Text: chunk}
	close(ch)
	cancelled := false
	res, err := a.consumeStream(context.Background(), session.SessionID("s_mix"),
		event.Actor{Kind: event.ActorAgent, ID: "x"}, ch, "m", "pt", "pr", func() { cancelled = true })
	if err != nil {
		t.Fatalf("consumeStream: %v", err)
	}
	if !res.reasoningSpun || !cancelled {
		t.Errorf("combined text+reasoning past the cap must spin+cancel (spun=%v cancelled=%v)", res.reasoningSpun, cancelled)
	}
	if len(res.text) > 100 || len(res.reasoning) > 100 {
		t.Errorf("neither channel alone should exceed the cap (text=%d reasoning=%d) — the sum is the trigger", len(res.text), len(res.reasoning))
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
