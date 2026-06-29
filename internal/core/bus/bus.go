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

// defaultBuffer is the per-subscriber channel buffer. A slow subscriber that fills
// its buffer drops only HIGH-VOLUME (droppable) events rather than blocking
// publishers; low-volume state transitions are preserved (see Publish).
const defaultBuffer = 1024

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

// Publish delivers ev to all current subscribers of ev.SessionID. It never blocks
// (all sends are non-blocking, so a stuck consumer can't stall publishers). On a
// full buffer: a droppable (high-volume) event is dropped; a must-deliver event
// (state transition / fact) instead EVICTS one buffered event to make room, so a
// critical transition like agent.status:"done" is never silently lost — losing it
// would desync the UI permanently (e.g. a subagent pane stuck "running").
func (b *Bus) Publish(ev event.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[ev.SessionID] {
		select {
		case ch <- ev:
			continue
		default:
		}
		if ev.Type.Droppable() {
			continue // best-effort for streaming deltas / progress ticks
		}
		// Must-deliver but full: drop the oldest buffered event (overwhelmingly a
		// droppable delta during a stream) and enqueue this one. Keeps the newest
		// state transition. Still non-blocking — no deadlock with cancel's write lock.
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- ev:
		default:
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

	// Close the subscription when the context is cancelled. A context with no
	// cancellation (e.g. context.Background) has a nil Done() channel — receiving on
	// it blocks forever, leaking this goroutine and, if every other goroutine also
	// parks, tripping the runtime's deadlock detector. Skip the watcher in that case;
	// the subscription then closes only via the explicit cancel() the caller defers.
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			cancel()
		}()
	}

	return ch, cancel
}

// SubscriberCount returns the number of active subscribers for sid (test aid).
func (b *Bus) SubscriberCount(sid session.SessionID) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[sid])
}
