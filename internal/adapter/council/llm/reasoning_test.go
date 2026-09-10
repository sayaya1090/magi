package llm

import (
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func streamOf(evs ...port.ProviderEvent) <-chan port.ProviderEvent {
	ch := make(chan port.ProviderEvent, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch
}

// What the model SAID and what it thought are two facts, and only one of them may be parsed.
//
// Only ProviderText was collected here, so a reply that arrived as reasoning alone left this
// function returning "" — and the callers reported "0 bytes" while the stream guard, which counts
// BOTH kinds toward its caps (`internal/app/provider_guard.go`), had just aborted a repetition loop
// it saw plenty of. One side of one stream saw thousands of characters and the other saw none.
//
// The reasoning is returned for the operator. It must never reach the parser: a model thinking out
// loud would otherwise become a verdict, which is the whole reason the two are kept apart.
func TestDrainKeepsReasoningApartFromTheAnswer(t *testing.T) {
	text, reasoning, cut := drain(streamOf(
		port.ProviderEvent{Type: port.ProviderReasoning, Text: "let me think: "},
		port.ProviderEvent{Type: port.ProviderText, Text: `{"decision":"done"}`},
		port.ProviderEvent{Type: port.ProviderReasoning, Text: "…and that is why"},
	))
	if cut != nil {
		t.Fatalf("끊기지 않았는데 사유가 왔다: %v", cut)
	}
	if text != `{"decision":"done"}` {
		t.Errorf("말한 것에 생각이 섞였다: %q", text)
	}
	if reasoning != "let me think: …and that is why" {
		t.Errorf("생각을 못 모았다: %q", reasoning)
	}

	// The case this exists for: reasoning only, and the stream aborted.
	text, reasoning, cut = drain(streamOf(
		port.ProviderEvent{Type: port.ProviderReasoning, Text: strings.Repeat("'s report is not a valid completion.\nThe agent", 90)},
		port.ProviderEvent{Type: port.ProviderError, Err: errors.New("a degenerate repetition loop")},
	))
	if text != "" {
		t.Errorf("답이 없었는데 답이 있다고 한다: %q", text)
	}
	if len(reasoning) < 1000 {
		t.Errorf("생각이 %d 자뿐이다 — 버려졌다", len(reasoning))
	}
	if cut == nil {
		t.Error("끊긴 사유가 안 왔다")
	}

	// And a verdict is never built from it. `parseReply` is what turns a reply into a vote; the
	// reasoning carries a JSON-shaped sentence here precisely so a leak would be visible.
	if _, ok := parseReply(text); ok {
		t.Error("답이 빈데 평결이 나왔다 — 생각이 파서로 샜다")
	}
	leak, _, _ := drain(streamOf(
		port.ProviderEvent{Type: port.ProviderReasoning, Text: `{"decision":"done","confidence":1}`},
	))
	if leak != "" {
		t.Errorf("생각만 온 답이 말한 것으로 셌다: %q", leak)
	}
}
