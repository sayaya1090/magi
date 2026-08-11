package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// appendFact persists a fact event (assigning seq) and publishes it on the bus.
func (a *App) appendFact(ctx context.Context, sid session.SessionID, typ event.Type, actor event.Actor, data json.RawMessage) error {
	ev := event.Event{SessionID: sid, Type: typ, Actor: actor, TS: time.Now(), Data: data}
	seqs, err := a.store.Append(ctx, sid, ev)
	if err != nil {
		return err
	}
	ev.Seq = seqs[0]
	a.touch(sid)
	a.bus.Publish(ev)
	return nil
}

// appendPromptText appends a single-text-part PromptSubmitted event to a session — the shared
// shape behind every "inject a note into a conversation" site (subagent Q&A, subagent results,
// refine success/failure records, plan-council notes, planner findings). Callers that must
// outlive the current turn pass context.WithoutCancel(ctx); the error is returned for the few
// sites that care and ignored (`_ =`) by the fire-and-forget ones.
func (a *App) appendPromptText(ctx context.Context, sid session.SessionID, actor event.Actor, text string) error {
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	return a.appendFact(ctx, sid, event.TypePromptSubmitted, actor, pd)
}

// publishTransient publishes a bus-only event (not persisted). No-op when the App
// was built without a bus (minimal test construction) — a transient event has no
// meaning with no subscribers.
func (a *App) publishTransient(sid session.SessionID, typ event.Type, actor event.Actor, data json.RawMessage) {
	if a.bus == nil {
		return
	}
	a.touch(sid)
	a.bus.Publish(event.Event{SessionID: sid, Type: typ, Actor: actor, TS: time.Now(), Data: data})
}

// emitToolProgress publishes a long-running tool's live progress note as a
// transient (bus-only, droppable) event so the TUI and headless stream can show
// what is being waited on. No-op when the bus is absent.
func (a *App) emitToolProgress(sid session.SessionID, actor event.Actor, callID, name, text string) {
	a.noteDoing(sid, callID, text)
	d, _ := json.Marshal(event.ToolProgressData{CallID: callID, Name: name, Text: text})
	a.publishTransient(sid, event.TypeToolProgress, actor, d)
}

// noteDoing keeps the latest progress note where a reader OUTSIDE this process can be told it.
//
// The bus reaches subscribers in this process — the terminal drawing its own daemon, the headless
// stream. A browser console is a different process reading the shared log, and a transient event
// is never written to the log, so from there a turn making steady progress and a turn wedged on a
// dead socket look the same. This is the only copy anybody else can ask for.
func (a *App) noteDoing(sid session.SessionID, callID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.doing, st.doingCall = text, callID
	a.mu.Unlock()
}

// clearDoing drops the note once the call that was reporting it has returned.
//
// Only that call's. A later call's note arriving first is not something to undo — the note names
// what is happening NOW, and a finished call clearing a running one's would blank the line at the
// moment it started being true.
func (a *App) clearDoing(sid session.SessionID, callID string) {
	a.mu.Lock()
	if st, ok := a.stateIf(sid); ok && st.doingCall == callID {
		st.doing, st.doingCall = "", ""
	}
	a.mu.Unlock()
}

// Doing is what a long-running tool last said it was waiting on, or "" when nothing has said.
//
// Read across the daemon socket beside Waiting, and for the same reason: both are live facts that
// exist only in the memory of the process holding the run. Waiting says a turn has stopped and
// needs a person; this says it has not stopped and what it is on.
func (a *App) Doing(sid session.SessionID) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok || st.doing == "" {
		return "", false
	}
	return st.doing, true
}

// NoteSessionMoved writes into a conversation that the companion has left it for another.
//
// Into the OLD session, deliberately: it is a fact about that conversation, and what a reader of
// it needs is the reason its transcript stops. The new one needs no mark — what happened there is
// that work carried on, which is what every event in it already says.
//
// Persisted AND on the bus, which is the whole mechanism by which other screens find out: a
// console streaming this session and a terminal attached to it are both already reading it, and a
// viewer who opens it next week reads the same line from the log.
func (a *App) NoteSessionMoved(ctx context.Context, from, to session.SessionID) error {
	d, err := json.Marshal(event.SessionMovedData{To: to})
	if err != nil {
		return err
	}
	return a.appendFact(ctx, from, event.TypeSessionMoved,
		event.Actor{Kind: event.ActorUser, ID: "console"}, d)
}
