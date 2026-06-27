// Package bus provides an in-memory publish/subscribe event bus with multi-
// subscriber fan-out, scoped per session. It is the live channel between the
// application and any UI (D5); persistence is handled separately by the Store.
package bus

import (
	"context"
	"sync"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// defaultBuffer is the per-subscriber channel buffer. A slow subscriber that
// fills its buffer drops events rather than blocking publishers (live UX is
// best-effort; durable history lives in the Store).
const defaultBuffer = 256

// Bus is a per-session fan-out event bus. The zero value is not usable; use New.
type Bus struct {
	mu     sync.RWMutex
	subs   map[session.SessionID]map[int]chan event.Event
	nextID int
	buffer int
}

// New returns an empty Bus.
func New() *Bus {
	return &Bus{
		subs:   make(map[session.SessionID]map[int]chan event.Event),
		buffer: defaultBuffer,
	}
}

// Publish delivers ev to all current subscribers of ev.SessionID. Delivery is
// non-blocking: if a subscriber's buffer is full, the event is dropped for that
// subscriber. Publish never blocks.
func (b *Bus) Publish(ev event.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[ev.SessionID] {
		select {
		case ch <- ev:
		default:
			// subscriber too slow; drop (Store remains source of truth)
		}
	}
}

// Subscribe registers a subscriber for events on sid. It returns a receive-only
// channel and a cancel func; the channel is closed when cancel is called or when
// ctx is done. Callers must drain or cancel to avoid leaking the goroutine that
// closes the channel.
func (b *Bus) Subscribe(ctx context.Context, sid session.SessionID) (<-chan event.Event, func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan event.Event, b.buffer)
	if b.subs[sid] == nil {
		b.subs[sid] = make(map[int]chan event.Event)
	}
	b.subs[sid][id] = ch
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if m := b.subs[sid]; m != nil {
				if c, ok := m[id]; ok {
					delete(m, id)
					close(c)
				}
				if len(m) == 0 {
					delete(b.subs, sid)
				}
			}
			b.mu.Unlock()
		})
	}

	// Close the subscription when the context is cancelled.
	go func() {
		<-ctx.Done()
		cancel()
	}()

	return ch, cancel
}

// SubscriberCount returns the number of active subscribers for sid (test aid).
func (b *Bus) SubscriberCount(sid session.SessionID) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[sid])
}
