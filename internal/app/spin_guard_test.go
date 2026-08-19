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
			"m", func() { cancelled = true })
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
		event.Actor{Kind: event.ActorAgent, ID: "x"}, ch, "m", func() { cancelled = true })
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

// The guard used to stand down when [limits] max_output_tokens was set, on the reading that the
// provider's token cap already bounded the response. It does — and that is not the same job. A cap
// bounds how BIG one response gets; the guard notices there is still no ACTION and tells the model
// to take one. Deferring kept the bound and threw away the recovery: the reply ends mid-thought at
// finish_reason "length", carries no tool call, and the next step begins exactly the same way with
// nothing having told the model to stop reasoning.
//
// Measured in an external run of the same model with a token cap configured (extract-elf and
// large-scale-text-editing, 2026-07-31): both turns reasoned into the cap step after step, made
// zero tool calls, never reached the council, and landed unverified with the deliverable never
// written. Two tasks, one missing nudge.
func TestSpinGuardStillFiresUnderAnOutputCap(t *testing.T) {
	t.Setenv("MAGI_SPIN_CAP", "100")
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow", MaxOutputTokens: 8000})
	ch := make(chan port.ProviderEvent, 10)
	for i := 0; i < 6; i++ {
		ch <- port.ProviderEvent{Type: port.ProviderReasoning, Text: "reasoning chunk of some length "}
	}
	close(ch)
	cancelled := false
	res, err := a.consumeStream(context.Background(), session.SessionID("s"),
		event.Actor{Kind: event.ActorAgent, ID: "x"}, ch, "m", func() { cancelled = true })
	if err != nil {
		t.Fatal(err)
	}
	if !res.reasoningSpun || !cancelled {
		t.Errorf("a token cap bounds the size, not the silence — the guard must still fire (spun=%v cancelled=%v)",
			res.reasoningSpun, cancelled)
	}
}

// Every spin gets a word, and no two are the same.
//
// The nudge was latched: say it once per turn, then cancel in silence. The reason was real — an
// identical instruction stacked on every step dilutes the attention the tool results need — but
// the conclusion cost more than it saved. Measured on schemelike-metacircular-eval (2026-08-19),
// NINE spins ten minutes apart with only the first carrying a word to the model: the other eight
// were cancelled with nothing said, so from the model's side each was a fresh question and it
// re-derived the same analysis eight times. 82 minutes, no tool call.
//
// So the rule is not "say it once", it is "never say the same thing twice".
func TestEverySpinIsToldAndNoTwoTellingsAreAlike(t *testing.T) {
	seen := map[string]int{}
	for n := 1; n <= 6; n++ {
		msg := reasoningSpinNudge(n)
		if strings.TrimSpace(msg) == "" {
			t.Fatalf("spin %d was cancelled with nothing said", n)
		}
		seen[msg]++
	}
	for msg, n := range seen {
		if n > 1 {
			t.Errorf("the same text was sent %d times — the Nth identical copy adds nothing and "+
				"dilutes what the tool results need: %.60s…", n, msg)
		}
	}
	// A repeat must carry what the model cannot see for itself: that its answer was cancelled and
	// the thinking discarded, and that this has happened before. Without those, a repeat is the
	// first message again in other words.
	second := reasoningSpinNudge(2)
	for _, want := range []string{"CANCELLED", "DISCARDED"} {
		if !strings.Contains(second, want) {
			t.Errorf("the second telling never says %q, so the model still cannot know why its "+
				"work keeps vanishing: %s", want, second)
		}
	}
	if !strings.Contains(reasoningSpinNudge(5), "5") {
		t.Error("a later telling does not say which spin this is; the model cannot tell one " +
			"cancellation from a pattern of them")
	}
}

// A todo list is not an action.
//
// The guard asked "did this response call any tool", and a todo write is a tool call that changes
// nothing outside the agent's own notes. Measured on regex-log (2026-08-19): 46 minutes, ONE tool
// call for the whole trial, and it was todowrite — issued right after a nudge saying to take a
// concrete step with a tool. The most literal possible compliance, and the turn produced nothing.
func TestPlanningIsNotActionForTheSpinGuard(t *testing.T) {
	t.Setenv("MAGI_SPIN_CAP", "100")
	a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	actor := event.Actor{Kind: event.ActorAgent, ID: "x"}
	consume := func(evs []port.ProviderEvent) streamStep {
		ch := make(chan port.ProviderEvent, len(evs)+1)
		for _, e := range evs {
			ch <- e
		}
		close(ch)
		res, err := a.consumeStream(context.Background(), session.SessionID("s_plan"), actor, ch, "m", func() {})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	long := func(first *session.ToolCall) []port.ProviderEvent {
		var evs []port.ProviderEvent
		if first != nil {
			evs = append(evs, port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: first})
		}
		for i := 0; i < 6; i++ {
			evs = append(evs, port.ProviderEvent{Type: port.ProviderReasoning, Text: "reasoning chunk of some length "})
		}
		return evs
	}

	if res := consume(long(&session.ToolCall{CallID: "c1", Name: "todowrite"})); !res.reasoningSpun {
		t.Error("a response that reasoned past the cap and only wrote a todo list was counted as " +
			"acting; planning is the failure this guard exists to interrupt, not an escape from it")
	}
	// A real action still clears it — including a READ, which is how a turn learns what to do next.
	for _, name := range []string{"bash", "write", "read"} {
		if res := consume(long(&session.ToolCall{CallID: "c2", Name: name})); res.reasoningSpun {
			t.Errorf("a response that called %q was cancelled as a spin", name)
		}
	}
}

// The work that was cancelled comes back with the nudge.
//
// The response is discarded — it is incomplete and ends mid-thought — but discarding it silently
// made the instruction impossible to follow. The nudge said "act on what you worked out" while the
// thing worked out existed nowhere in the model's context, so the only move left was to derive it
// again, which is the loop the guard exists to break. Measured on regex-log (2026-08-19): a
// correct, nearly complete regex design cancelled and thrown away, and the next response opened
// the same analysis from the top.
func TestTheCancelledWorkComesBackWithTheNudge(t *testing.T) {
	// The tail, not the head: a chain of reasoning puts its conclusions at the end and restates the
	// problem at the start.
	long := strings.Repeat("restating the problem. ", 300) + "CONCLUSION: the pattern is ^a+b$"
	tail := salvageTail("", long)
	if !strings.Contains(tail, "CONCLUSION: the pattern is ^a+b$") {
		t.Error("the conclusion was cut off; salvage keeps the end, which is where conclusions are")
	}
	if len(tail) > salvageCap+8 {
		t.Errorf("salvage is %d bytes; it must stay bounded or a spin loop grows the prompt it is trying to break", len(tail))
	}
	// Nothing produced → nothing claimed. Offering an empty "here is what you had" would be a lie
	// the model then tries to act on.
	if got := salvageTail("", "   \n  "); got != "" {
		t.Errorf("an empty response salvaged %q", got)
	}
	// Short output survives whole.
	if got := salvageTail("I had worked out the octet pattern", ""); got != "I had worked out the octet pattern" {
		t.Errorf("short work was mangled: %q", got)
	}
}
