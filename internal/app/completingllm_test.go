package app

import (
	"context"

	"github.com/sayaya1090/magi/internal/port"
)

// completingLLM emits a single terminal text turn (no tool calls) and closes, so a session driven
// by it finishes immediately with a trivial reply.
type completingLLM struct{}

func (completingLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "Hi! How can I help?"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}
