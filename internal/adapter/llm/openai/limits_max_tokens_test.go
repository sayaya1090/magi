package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// [limits] max_output_tokens is the operator's cap on a single response. It reaches the provider
// as `max_tokens`, and it has to be absent — not zero — when nothing was configured, or a provider
// reading `"max_tokens": 0` would answer with nothing at all.
func TestMaxOutputTokensReachesTheWireAndZeroDoesNot(t *testing.T) {
	req := port.ChatRequest{Model: "m", Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}}}}}

	body := buildRequest(req, false, false, "", 4096, Sampling{}, nil)
	if body.MaxTokens != 4096 {
		t.Fatalf("the configured cap is carried, got %d", body.MaxTokens)
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"max_tokens":4096`) {
		t.Errorf("and serialized under the provider's name:\n%s", b)
	}

	// Unset means unset: omitempty must drop the field rather than send a zero cap.
	b, err = json.Marshal(buildRequest(req, false, false, "", 0, Sampling{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "max_tokens") {
		t.Errorf("no cap configured, so the provider's own default must stand:\n%s", b)
	}
}
