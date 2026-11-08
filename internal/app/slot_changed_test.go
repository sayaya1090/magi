package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
)

func TestNotifySlotChangedReachesAWatchedSession(t *testing.T) {
	a := &App{bus: bus.New()}
	ch, stop := a.bus.Subscribe(context.Background(), "s1")
	defer stop()
	a.NotifySlotChanged("knowledge", "engram")
	select {
	case ev := <-ch:
		if ev.Type != event.TypeSlotChanged || ev.SessionID != "s1" {
			t.Fatalf("got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("slot.changed never arrived")
	}
}
