package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Shutdown is Close with a deadline, for a process that is going away. It stops new work, cancels
// every run and waits for them up to grace. Past that it returns the sessions whose runs had not
// settled, so the caller can say what was left rather than hang the process — and every deferred
// cleanup behind it (plugins, sockets) — on one tool or backend that ignores its cancellation.
// What those runs leave in the log is settled by the next start's recovery, not here.
func (a *App) Shutdown(grace time.Duration) []session.SessionID {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if a.Close(ctx) == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var left []session.SessionID
	for id, st := range a.states {
		if st.cancel != nil {
			left = append(left, id)
		}
	}
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	return left
}

// noteShutdownInterrupt records that a turn was cut by the process shutting down: interrupted and
// resumable, the same ending a transient backend failure gets, so the next start (and anyone
// reading the log) can tell it from a turn the person cancelled.
func (a *App) noteShutdownInterrupt(ctx context.Context, sid session.SessionID) {
	a.mu.Lock()
	st := a.stateLocked(sid)
	cut := st.shutdownCut
	st.shutdownCut = nil
	a.mu.Unlock()
	data := event.ErrorData{Code: "interrupted", Resumable: len(cut) == 0,
		Message: "interrupted: magi shut down mid-turn. Nothing after the last saved step was lost or re-run — " +
			"send a follow-up such as \"continue\" to pick it up"}
	if len(cut) > 0 {
		data.Message = fmt.Sprintf("interrupted: magi shut down while %d tool call(s) were running (%s); they may have run. "+
			"Nothing was re-run — check their effect, then say how to continue", len(cut), strings.Join(cut, ", "))
	}
	b, err := json.Marshal(data)
	if err == nil {
		a.appendBestEffort(ctx, sid, event.TypeError, event.Actor{Kind: event.ActorSystem, ID: "loop"}, b)
	}
}

// cutByShutdown reports whether sid's run is being ended by the process going away rather than by
// a person pressing stop.
func (a *App) cutByShutdown(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed && a.stateLocked(sid).cancelSweep == nil
}

// noteShutdownCut remembers a call a shutdown cut mid-run, for noteShutdownInterrupt.
func (a *App) noteShutdownCut(sid session.SessionID, callID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	st.shutdownCut = append(st.shutdownCut, callID)
}
