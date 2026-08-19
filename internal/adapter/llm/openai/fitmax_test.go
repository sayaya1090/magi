package openai

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// max_output_tokens is a CEILING, and it was going out as a DEMAND.
//
// Reported from a live run: a 393,216-token window, 93,217 tokens of input, and a configured cap
// of 300,000 — which is 393,217. One token over, the backend refused the request, and the agent
// exited non-zero with the task lost. The two numbers could never meet: the window is resolved in
// the app and the cap is applied at the wire.
func TestAConfiguredCapIsHeldUnderTheWindow(t *testing.T) {
	big := strings.Repeat("x", 400_000) // ≈100k tokens at chars/4
	req := port.ChatRequest{
		Model:    "m",
		Messages: []session.Message{{Parts: []session.Part{{Kind: session.PartText, Text: big}}}},
	}
	c := &Client{maxTokens: 300_000, window: func(string) int { return 393_216 }}

	got := c.fitMaxTokens(req)
	if got >= 300_000 {
		t.Fatalf("the cap went out as %d — unchanged, which is the demand that killed the turn", got)
	}
	if in := estimateRequestTokens(req); in+got > 393_216 {
		t.Errorf("input %d + cap %d = %d, past the window; fitting it is the whole job", in, got, in+got)
	}
	if got < minOutputTokens {
		t.Errorf("the cap was cut to %d, too small to carry a tool call and its arguments", got)
	}
}

// It only ever LOWERS. A short conversation on a big window keeps exactly what was asked for —
// the operator's number is a ceiling, and lowering it when there is room would be this code
// overriding a decision that was not its to make.
func TestRoomToSpareLeavesTheCapAlone(t *testing.T) {
	req := port.ChatRequest{Model: "m", Messages: []session.Message{
		{Parts: []session.Part{{Kind: session.PartText, Text: "hello"}}}}}
	c := &Client{maxTokens: 8192, window: func(string) int { return 393_216 }}
	if got := c.fitMaxTokens(req); got != 8192 {
		t.Errorf("a cap of 8192 with a whole window free came out as %d", got)
	}
}

// With no cap configured, nothing is sent — before and after. Computing one where the operator
// asked for none would change what every backend receives, and some behave differently with an
// explicit max_tokens than without one.
func TestNoConfiguredCapStaysUnsent(t *testing.T) {
	req := port.ChatRequest{Model: "m"}
	c := &Client{maxTokens: 0, window: func(string) int { return 1024 }}
	if got := c.fitMaxTokens(req); got != 0 {
		t.Errorf("an unset cap became %d; the field must stay off the wire", got)
	}
}

// An unknown window changes nothing. Most backends never say how big their context is, and
// guessing one here would cut a cap the operator set on a model we simply cannot measure.
func TestAnUnknownWindowLeavesTheCapAlone(t *testing.T) {
	req := port.ChatRequest{Model: "m"}
	for _, w := range []func(string) int{nil, func(string) int { return 0 }} {
		c := &Client{maxTokens: 64_000, window: w}
		if got := c.fitMaxTokens(req); got != 64_000 {
			t.Errorf("with no window known the cap became %d", got)
		}
	}
}
