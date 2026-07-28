package app

import (
	"context"
	"sync"

	"github.com/sayaya1090/magi/internal/port"
)

// auditLLM answers the check-audit side call with a scripted sequence and records the System prompt
// of every call, so a test can assert BOTH what came back and what was asked the second time.
type auditLLM struct {
	mu      sync.Mutex
	replies []string
	systems []string
}

func (f *auditLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	n := len(f.systems)
	f.systems = append(f.systems, r.System)
	text := "done"
	if n < len(f.replies) {
		text = f.replies[n]
	}
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 4)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: text}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (f *auditLLM) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.systems...)
}
