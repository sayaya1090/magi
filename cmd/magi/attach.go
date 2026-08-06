package main

import (
	"context"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// attached is the engine an attached UI talks to: its own App for everything it can work out for
// itself, and the daemon for the handful of things only the process holding the run can do.
//
// The split is not a compromise. The session log is append-only and the store is a port, so a
// second process reading the same directory reconstructs the same transcript — that is the store
// port used twice, which is what having ports is for. What it cannot reconstruct is the run: the
// goroutine, its cancel, the tool blocked waiting for an answer. Those five go over the wire.
//
// Embedding *app.App means every method not named below is the local one, and a method added to
// the boundary later keeps working here without being listed. The five are overridden by being
// written out, so the file reads as "these, and only these, leave the process".
type attached struct {
	*app.App
	c *daemon.Client
}

// Submit and Steer start or redirect work. Both must happen where the loop runs; a local Submit
// would start a SECOND agent against the same store, and the two would fight over one workspace.
func (a attached) Submit(ctx context.Context, c command.SubmitPrompt) error {
	return a.c.Submit(ctx, c)
}

func (a attached) Steer(ctx context.Context, c command.SubmitPrompt) error {
	return a.c.Steer(ctx, c)
}

// Interrupt cancels the live turn. The cancel function is in the daemon's memory and nowhere else,
// so a local call would return cleanly and stop nothing — the worst shape a control has.
func (a attached) Interrupt(ctx context.Context, c command.Interrupt) error {
	return a.c.Interrupt(ctx, c)
}

// RespondPermission and RespondQuestion answer a tool that is blocked. The channel it waits on
// belongs to the daemon; answering locally would leave it waiting forever while the screen showed
// the question resolved.
func (a attached) RespondPermission(ctx context.Context, c command.RespondPermission) error {
	return a.c.RespondPermission(ctx, c)
}

func (a attached) RespondQuestion(ctx context.Context, c command.RespondQuestion) error {
	return a.c.RespondQuestion(ctx, c)
}

// pollInterval is how often an attached UI re-reads the store. The daemon's bus is in the daemon's
// memory, so this is the only way live work reaches a second process. Fast enough that a tool call
// appears while you are still looking at it; slow enough that watching costs nothing.
const pollInterval = 300 * time.Millisecond

// Subscribe is the one READ that cannot be answered locally the ordinary way.
//
// App.Subscribe hands back the store's past plus the in-process bus's future, and this process's
// bus is empty — the events are published in the daemon. What both processes DO share is the log,
// so this polls it: ask for everything after the last sequence seen, forward whatever is new, wait,
// ask again.
//
// Polling rather than a second event socket, for the same reason only five calls cross at all. The
// log is append-only and already the record of what happened; a parallel stream of the same facts
// would be a second copy to keep honest, and the first thing to disagree after a reconnect.
func (a attached) Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error) {
	// The first read is synchronous so a caller that gets a channel also knows the session exists —
	// the same contract App.Subscribe has, where a bad session id is an error and not an empty feed.
	first, stopFirst, err := a.App.Subscribe(ctx, sid, fromSeq)
	if err != nil {
		return nil, nil, err
	}
	pctx, stopPolling := context.WithCancel(ctx)
	cancel := func() { stopPolling(); stopFirst() }
	out := make(chan event.Event)
	go func() {
		defer close(out)
		seq := fromSeq
		send := func(src <-chan event.Event) bool {
			for e := range src {
				if e.Seq != 0 && e.Seq <= seq {
					continue // already delivered on an earlier poll
				}
				if e.Seq > seq {
					seq = e.Seq
				}
				select {
				case out <- e:
				case <-pctx.Done():
					return false
				}
			}
			return true
		}
		// The first subscription's live half never fires here (nothing publishes in this process),
		// but its past half is the backlog, so it is drained like any other poll.
		if !send(drainPast(pctx, first)) {
			return
		}
		tick := time.NewTicker(pollInterval)
		defer tick.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case <-tick.C:
			}
			ch, stop, err := a.App.Subscribe(pctx, sid, seq)
			if err != nil {
				continue // a transient read failure is not the end of watching
			}
			ok := send(drainPast(pctx, ch))
			stop() // release this poll subscription before the next one
			if !ok {
				return
			}
		}
	}()
	return out, cancel, nil
}

// drainPast takes only what a subscription already has and then stops waiting on it.
//
// App.Subscribe's channel stays open on the in-process bus, which in this process never speaks —
// so reading it to completion would block forever. The backlog arrives without a gap between
// events; a pause means there is nothing more in the log yet, which is exactly when this poll is
// finished and the next one should start.
func drainPast(ctx context.Context, src <-chan event.Event) <-chan event.Event {
	out := make(chan event.Event)
	go func() {
		defer close(out)
		idle := time.NewTimer(pollInterval / 3)
		defer idle.Stop()
		for {
			select {
			case e, ok := <-src:
				if !ok {
					return
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(pollInterval / 3)
			case <-idle.C:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
