package bus

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

func TestPublishAllReachesEverySessionStampedWithItsID(t *testing.T) {
	b := New()
	a, stopA := b.Subscribe(context.Background(), "a")
	defer stopA()
	z, stopZ := b.Subscribe(context.Background(), "z")
	defer stopZ()
	b.PublishAll(event.Event{Type: event.TypeSlotChanged})
	if ev := <-a; ev.SessionID != "a" || ev.Type != event.TypeSlotChanged {
		t.Fatalf("a got %+v", ev)
	}
	if ev := <-z; ev.SessionID != "z" {
		t.Fatalf("z got %+v", ev)
	}
}

func TestPublishWithoutASessionReachesNobody(t *testing.T) {
	b := New()
	a, stop := b.Subscribe(context.Background(), "a")
	defer stop()
	b.Publish(event.Event{Type: event.TypeSlotChanged})
	select {
	case ev := <-a:
		t.Fatalf("unexpected %+v", ev)
	default:
	}
}
