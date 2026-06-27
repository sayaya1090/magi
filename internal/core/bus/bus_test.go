package bus

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func ev(sid session.SessionID, t event.Type) event.Event {
	return event.Event{SessionID: sid, Type: t}
}

// Multi-subscriber fan-out: every subscriber of a session receives a published
// event (D5 — supports multiple UIs / late joiners on the live channel).
func TestFanOut(t *testing.T) {
	b := New()
	ctx := context.Background()
	ch1, cancel1 := b.Subscribe(ctx, "s1")
	ch2, cancel2 := b.Subscribe(ctx, "s1")
	defer cancel1()
	defer cancel2()

	b.Publish(ev("s1", event.TypePartDelta))

	for i, ch := range []<-chan event.Event{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Type != event.TypePartDelta {
				t.Errorf("sub%d: got %q, want %q", i, got.Type, event.TypePartDelta)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub%d: timed out waiting for event", i)
		}
	}
}

// Events are scoped per session: a subscriber only sees its own session.
func TestSessionScoping(t *testing.T) {
	b := New()
	ctx := context.Background()
	ch, cancel := b.Subscribe(ctx, "s1")
	defer cancel()

	b.Publish(ev("s2", event.TypePartDelta)) // different session

	select {
	case got := <-ch:
		t.Fatalf("unexpected event for other session: %+v", got)
	case <-time.After(50 * time.Millisecond):
		// expected: nothing delivered
	}
}

// Cancel unsubscribes and closes the channel; later publishes are not delivered.
func TestCancelUnsubscribes(t *testing.T) {
	b := New()
	ctx := context.Background()
	ch, cancel := b.Subscribe(ctx, "s1")

	if got := b.SubscriberCount("s1"); got != 1 {
		t.Fatalf("SubscriberCount=%d, want 1", got)
	}
	cancel()
	if got := b.SubscriberCount("s1"); got != 0 {
		t.Fatalf("after cancel SubscriberCount=%d, want 0", got)
	}

	// Channel should be closed.
	if _, open := <-ch; open {
		t.Errorf("channel should be closed after cancel")
	}

	// Publishing after cancel must not panic.
	b.Publish(ev("s1", event.TypePartDelta))
}

// Context cancellation closes the subscription.
func TestContextCancelCloses(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := b.Subscribe(ctx, "s1")

	cancel()

	select {
	case _, open := <-ch:
		if open {
			t.Errorf("channel should be closed after ctx cancel")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for channel close on ctx cancel")
	}
}

// A slow subscriber that fills its buffer drops events instead of blocking the
// publisher (Publish never blocks).
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	b := New()
	ctx := context.Background()
	_, cancel := b.Subscribe(ctx, "s1") // never drained
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < defaultBuffer*4; i++ {
			b.Publish(ev("s1", event.TypePartDelta))
		}
		close(done)
	}()

	select {
	case <-done:
		// publisher completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatalf("Publish blocked on a full subscriber buffer")
	}
}
