package app

import (
	"context"
	"sync/atomic"

	"github.com/sayaya1090/magi/internal/port"
)

// gateLLM is a fake provider whose StreamChat blocks on a gate (to hold agents
// "running") and then returns a fixed text turn.
type gateLLM struct {
	active  atomic.Int32
	maxSeen atomic.Int32
	gate    chan struct{}
	text    string
}

func (f *gateLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	n := f.active.Add(1)
	for {
		m := f.maxSeen.Load()
		if n <= m || f.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	ch := make(chan port.ProviderEvent, 4)
	go func() {
		defer f.active.Add(-1)
		if f.gate != nil {
			select {
			case <-f.gate:
			case <-ctx.Done():
			}
		}
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: f.text}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
		close(ch)
	}()
	return ch, nil
}
