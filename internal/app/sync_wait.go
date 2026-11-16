package app

import (
	"context"
	"log"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// syncBeforeWaiting forces the session's log to stable storage before magi waits on a person. The
// store syncs only at turn boundaries (jsonl.durableBoundary); a person may take minutes to answer,
// and a power cut in that time should not take the steps that led to the question. A failure is
// logged, not raised: the events are already with the OS, and the person is still asked.
func (a *App) syncBeforeWaiting(ctx context.Context, sid session.SessionID) {
	var s port.Store = a.store
	if b, ok := s.(bornStore); ok {
		s = b.Store
	}
	ls, ok := s.(port.LogSyncer)
	if !ok {
		return
	}
	if err := ls.SyncLog(ctx, sid); err != nil {
		log.Printf("magi: syncing %s before asking: %v", sid, err)
	}
}
