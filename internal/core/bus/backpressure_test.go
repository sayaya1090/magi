package bus

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A must-deliver event (a low-volume state transition like agent.status) must NOT
// be dropped even when a slow consumer has filled the buffer with droppable deltas
// — losing it would desync the UI permanently (e.g. a subagent pane stuck running).
func TestPublishPreservesMustDeliverUnderBackpressure(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(context.Background(), "s1")
	defer cancel()

	// Flood with droppable deltas and never consume → buffer fills, excess dropped.
	for i := 0; i < defaultBuffer+100; i++ {
		b.Publish(event.Event{SessionID: "s1", Type: event.TypePartDelta})
	}
	// A critical transition arrives while the buffer is full.
	b.Publish(event.Event{SessionID: "s1", Type: event.TypeAgentStatus})

	// Drain non-blockingly; the agent.status must be in there (displaced a delta).
	found := false
	for {
		select {
		case e := <-ch:
			if e.Type == event.TypeAgentStatus {
				found = true
			}
			continue
		default:
		}
		break
	}
	if !found {
		t.Fatal("must-deliver agent.status was dropped under backpressure (stuck-pane bug)")
	}
}

// A droppable event, by contrast, IS dropped when the buffer is full (best-effort).
func TestPublishDropsDroppableWhenFull(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(context.Background(), "s1")
	defer cancel()
	for i := 0; i < defaultBuffer+100; i++ {
		b.Publish(event.Event{SessionID: "s1", Type: event.TypePartDelta})
	}
	// Buffer holds EXACTLY defaultBuffer events: the first fill it, the excess deltas
	// are dropped (not blocked, not grown). `len > cap` is impossible in Go, so the only
	// meaningful assertion is that it's full — proving drops happened, not expansion.
	if n := len(ch); n != defaultBuffer {
		t.Fatalf("buffer should be full at %d (excess dropped), got %d", defaultBuffer, n)
	}
}
