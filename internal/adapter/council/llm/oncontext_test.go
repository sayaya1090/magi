package llm

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// On-context mode exists for one measured reason: today each member sends a differently-headed
// prompt, so no two share a prefix and none of the nine council sessions in a recorded trial read
// a single token from cache. Giving every member the same conversation and differing only in the
// last message makes the shared part actually shared.
//
// The property that has to hold for that to pay is byte-level: the three requests must be
// identical up to the final message, and identical to what the agent itself sent.

type capturingProvider struct {
	mu   sync.Mutex
	reqs []port.ChatRequest
}

func (c *capturingProvider) StreamChat(_ context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, r)
	c.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText,
		Text: `{"decision":"done","confidence":0.9,"rationale":"looks complete","cite":"NO-EVIDENCE"}`}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// snapshot returns the captured requests in a stable order: members are polled concurrently, so
// arrival order is not the member order, and a byte comparison needs a deterministic list.
func (c *capturingProvider) snapshot() []port.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]port.ChatRequest(nil), c.reqs...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Messages[len(out[i].Messages)-1].Parts[0].Text <
			out[j].Messages[len(out[j].Messages)-1].Parts[0].Text
	})
	return out
}

func convo() []session.Message {
	return []session.Message{
		{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "make the tests pass"}}},
		{Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartText, Text: "I ran them and fixed the parser"}}},
	}
}

func TestOnContextMembersShareEveryByteButTheLast(t *testing.T) {
	cap := &capturingProvider{}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return cap }}
	_, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "make the tests pass", Context: convo(), ContextSystem: "SESSION SYSTEM PROMPT",
		Members: council.DefaultMembers(),
	})
	if err != nil {
		t.Fatal(err)
	}
	reqs := cap.snapshot()
	if len(reqs) < 3 {
		t.Fatalf("expected one request per member, got %d", len(reqs))
	}
	first := reqs[0]
	for i, r := range reqs[1:] {
		if r.System != first.System {
			t.Fatalf("member %d sent a different system prompt — the prefix is shared only up to "+
				"the first differing byte, and that is byte one", i+1)
		}
		if len(r.Messages) != len(first.Messages) {
			t.Fatalf("member %d sent %d messages, first sent %d", i+1, len(r.Messages), len(first.Messages))
		}
		for j := 0; j < len(r.Messages)-1; j++ {
			if r.Messages[j].Parts[0].Text != first.Messages[j].Parts[0].Text {
				t.Fatalf("member %d differs at message %d, before the tail — nothing before the "+
					"lens may vary", i+1, j)
			}
		}
		if r.Messages[len(r.Messages)-1].Parts[0].Text == first.Messages[len(first.Messages)-1].Parts[0].Text {
			t.Fatalf("member %d sent the same tail as the first — the lens is what makes it a "+
				"different judgement", i+1)
		}
	}
	// And the shared part is the agent's own conversation, verbatim.
	if first.System != "SESSION SYSTEM PROMPT" {
		t.Fatalf("the head should be the session's own system prompt, got %q", first.System)
	}
	if first.Messages[0].Parts[0].Text != "make the tests pass" {
		t.Fatalf("the conversation should lead, got %q", first.Messages[0].Parts[0].Text)
	}
}

func TestOnContextTailCarriesLensAndTheClaimWarning(t *testing.T) {
	cap := &capturingProvider{}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return cap }}
	_, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "ship it", Context: convo(), ContextSystem: "SYS",
		Members: []council.Member{{Name: "one", Lens: council.DefaultMembers()[0].Lens}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqs := cap.snapshot()
	tail := reqs[0].Messages[len(reqs[0].Messages)-1].Parts[0].Text
	for _, want := range []string{"COUNCIL", "ship it", "YOUR LENS", "claim, not evidence", "decision"} {
		if !strings.Contains(tail, want) {
			t.Fatalf("the tail must carry %q — it is the only part of the request the member has "+
				"not already seen:\n%s", want, tail)
		}
	}
}

// Without Context the old path is untouched: an assembled evidence block, one message, the lens in
// the system prompt. The two modes are compared on a bench, so neither may drift into the other.
func TestWithoutContextTheAssembledPathIsUnchanged(t *testing.T) {
	cap := &capturingProvider{}
	c := &Council{model: "m", resolve: func(string) port.LLMProvider { return cap }}
	_, err := c.Deliberate(context.Background(), port.DeliberationRequest{
		Task: "ship it", Actions: "wrote 13 bytes to hello.txt",
		Members: []council.Member{{Name: "one", Lens: council.DefaultMembers()[0].Lens}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := cap.snapshot()[0]
	if len(r.Messages) != 1 {
		t.Fatalf("the assembled path sends exactly one message, got %d", len(r.Messages))
	}
	if !strings.Contains(r.System, "lens") && !strings.Contains(strings.ToLower(r.System), "judge") {
		t.Fatalf("the assembled path keeps the lens in the system prompt, got %q", r.System)
	}
	if !strings.Contains(r.Messages[0].Parts[0].Text, "hello.txt") {
		t.Fatal("the assembled path judges on the evidence block")
	}
}
