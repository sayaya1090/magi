package app

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/sayaya1090/magi/internal/port"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// backgroundActor leaves the note a finished background job brings back to the model.
var backgroundActor = event.Actor{Kind: event.ActorSystem, ID: "background"}

// wakeOnBackground brings a finished background job back to the agent (2026-11-17: "when the
// background wait ends the result should reach you and you carry on"). It used to leave only a
// tool.background fact for the screens, so the model learned of it when the person next spoke.
// A note says how the job ended and a turn starts to act on it. A job that ends while a turn runs
// (or at the instant one ends) is held and delivered when that turn has fully ended, not dropped.
func (a *App) wakeOnBackground(sid session.SessionID, callID, state string, status port.BackgroundCompletion) {
	note := fmt.Sprintf("[background job finished] The background work started by call %s has ended: %s, exit %d.", callID, state, status.Exit)
	if status.Detail == "" {
		note += " Read its output (bash_output) and continue the task from where it stood."
	} else {
		note += "\n" + status.Detail
	}
	a.mu.Lock()
	if a.closed || a.holding {
		a.mu.Unlock()
		return
	}
	st := a.stateLocked(sid)
	if st.cancel != nil {
		st.pendingBackground = append(st.pendingBackground, note)
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.deliverBackgroundNotes(sid, []string{note})
}

// deliverBackgroundNotes records each note for the model and starts a turn to act on them.
func (a *App) deliverBackgroundNotes(sid session.SessionID, notes []string) {
	if len(notes) == 0 {
		return
	}
	a.mu.Lock()
	closed := a.closed || a.holding
	a.mu.Unlock()
	if closed {
		return
	}
	ctx := context.Background()
	for _, n := range notes {
		if err := a.appendPromptText(ctx, sid, backgroundActor, n); err != nil {
			return
		}
	}
	a.startRun(ctx, sid)
}

// promptArrived is the channel that closes on the person's next message in sid (ToolEnv.PromptArrived).
func (a *App) promptArrived(sid session.SessionID) <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	if st.promptWake == nil {
		st.promptWake = make(chan struct{})
	}
	return st.promptWake
}

// wakePromptWaiters ends every wait on sid's next message.
func (a *App) wakePromptWaiters(sid session.SessionID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if ok && st.promptWake != nil {
		close(st.promptWake)
		st.promptWake = nil
	}
}

// startToolBackground admits one worker and ties it to session interruption and app shutdown.
// The worker waits until its launch result is recorded before it reports progress or completes.
func (a *App) startToolBackground(ctx context.Context, sid session.SessionID, callID string, ready <-chan struct{}, work func(context.Context) port.BackgroundCompletion) (<-chan port.BackgroundCompletion, context.CancelFunc, func(), error) {
	a.mu.Lock()
	if a.closed || a.holding || ctx.Err() != nil {
		a.mu.Unlock()
		return nil, nil, nil, fmt.Errorf("the application or starting turn is stopping")
	}
	jobCtx, cancel := context.WithCancel(a.bgContext())
	if a.backgroundCancels == nil {
		a.backgroundCancels = make(map[session.SessionID]map[string]context.CancelFunc)
	}
	if a.backgroundCancels[sid] == nil {
		a.backgroundCancels[sid] = make(map[string]context.CancelFunc)
	}
	a.backgroundCancels[sid][callID] = cancel
	a.wg.Add(1)
	a.mu.Unlock()
	done := make(chan port.BackgroundCompletion, 1)
	go func() {
		defer func() {
			if crash := recover(); crash != nil {
				done <- port.BackgroundCompletion{Exit: 1, Detail: fmt.Sprintf("background tool panicked: %v\n%s", crash, firstFrames(string(debug.Stack()), 8))}
			}
			close(done)
		}()
		<-ready
		if jobCtx.Err() != nil {
			done <- port.BackgroundCompletion{Killed: true}
			return
		}
		status := work(jobCtx)
		status.Killed = status.Killed || jobCtx.Err() != nil
		done <- status
	}()
	return done, cancel, func() {
		cancel()
		a.mu.Lock()
		delete(a.backgroundCancels[sid], callID)
		if len(a.backgroundCancels[sid]) == 0 {
			delete(a.backgroundCancels, sid)
		}
		a.mu.Unlock()
		a.wg.Done()
	}, nil
}

func backgroundDetail(detail string) string {
	if strings.TrimSpace(detail) == "" {
		return "The tool returned no output."
	}
	return string(capToolResult([]byte(detail)))
}
