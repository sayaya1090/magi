package app

import (
	"context"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// bornStore writes a session's held session.created fact just before the first event that follows
// it, whichever door that event came through.
//
// The deferral itself is in CreateSession: the id is issued at once, and the fact waits so that a
// conversation nobody speaks in leaves nothing on disk. That only works if EVERY append flushes it,
// and appends do not all pass through appendFact — fork copies a batch, and a good deal of the test
// suite writes through the store directly. Putting the flush at the store seam rather than at one
// call site is what makes "the session is created when it first has something in it" a property of
// the store instead of a rule each caller has to remember.
//
// The store requires a session's first append to carry session.created, so the flush has to be its
// own Append and has to come first.
type bornStore struct {
	port.Store
	app *App
}

func (b bornStore) Append(ctx context.Context, sid session.SessionID, evs ...event.Event) ([]int64, error) {
	// A batch that already opens with the fact is a session being written whole (a fork). Nothing
	// to flush, and flushing would write the fact twice.
	if len(evs) > 0 && evs[0].Type == event.TypeSessionCreated {
		b.app.forget(sid)
		return b.Store.Append(ctx, sid, evs...)
	}
	if err := b.app.bear(ctx, sid); err != nil {
		return nil, err
	}
	return b.Store.Append(ctx, sid, evs...)
}
