package app

import (
	"context"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/port"
)

// recLLM records the full text (system + messages) of every request and replies with
// reply(req) — enabling assertions on which prompts were issued (e.g. did the failure
// retry send a decomposition-framed prompt) and content-driven success/failure.
type recLLM struct {
	mu      sync.Mutex
	prompts []string
	reply   func(req string) string // nil → always empty (every attempt "fails")
}

func (r *recLLM) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	var b strings.Builder
	b.WriteString(req.System)
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			b.WriteString(p.Text)
		}
	}
	s := b.String()
	r.mu.Lock()
	r.prompts = append(r.prompts, s)
	r.mu.Unlock()
	out := ""
	if r.reply != nil {
		out = r.reply(s)
	}
	ch := make(chan port.ProviderEvent, 4)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: out}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}
