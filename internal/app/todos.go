package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// SetTodos replaces a session's plan in memory only (no event) — used to clear the
// plan on a new prompt. Observable mutations go through putTodos.
func (a *App) SetTodos(sid session.SessionID, td []session.Todo) {
	a.mu.Lock()
	a.stateLocked(sid).todos = td
	a.mu.Unlock()
}

// putTodos replaces a session's plan and, when it actually changed, records a
// TodosChanged fact — so the plan's progression is logged, replayable, and re-renders
// the panel. appendFact MUST be called outside a.mu (it re-locks via currentStage).
func (a *App) putTodos(ctx context.Context, sid session.SessionID, actor event.Actor, td []session.Todo) {
	a.mu.Lock()
	if todosEqual(a.stateLocked(sid).todos, td) {
		a.mu.Unlock()
		return
	}
	a.stateLocked(sid).todos = td
	a.mu.Unlock()
	d, _ := json.Marshal(event.TodosChangedData{Todos: td})
	_ = a.appendFact(ctx, sid, event.TypeTodosChanged, actor, d)
}

// setTodoStatusIf moves the i-th todo from one status to another, but only when it is
// currently `from` — so a caller can't downgrade a completed/cancelled step. Used to
// start a step (pending→in_progress) and to revert it (in_progress→pending) if its
// pre-flight exploration produced nothing. Copy-on-write under one lock; fact emitted
// outside it, only on a real change.
func (a *App) setTodoStatusIf(ctx context.Context, sid session.SessionID, actor event.Actor, i int, from, to string) {
	a.mu.Lock()
	td := a.stateLocked(sid).todos
	if i < 0 || i >= len(td) || td[i].Status != from {
		a.mu.Unlock()
		return
	}
	cp := append([]session.Todo(nil), td...)
	cp[i].Status = to
	a.stateLocked(sid).todos = cp
	a.mu.Unlock()
	d, _ := json.Marshal(event.TodosChangedData{Todos: cp})
	_ = a.appendFact(ctx, sid, event.TypeTodosChanged, actor, d)
}

// markTodoActive moves the i-th todo pending→in_progress (◐) so the panel shows which
// step is running; only a pending step is started, never a completed/cancelled one.
func (a *App) markTodoActive(ctx context.Context, sid session.SessionID, actor event.Actor, i int) {
	a.setTodoStatusIf(ctx, sid, actor, i, "pending", "in_progress")
}

// markFirstPendingActive marks the first still-pending todo in_progress, so once
// pre-flight has checked off what it ran, the panel shows the main agent working the
// next step (◐) for the rest of the turn (finalizeTodos resolves it on exit).
//
// Note: a PURE-SOLO plan (no scout/parallel step) gets no per-step pre-flight signal —
// the main agent runs all of it inline with no step boundary — so only the first step
// shows ◐ and a mid-run cancel marks the rest cancelled even if an early step was in
// fact done. That's an accepted limitation: without the model calling todowrite there
// is no deterministic signal for solo-step completion.
func (a *App) markFirstPendingActive(ctx context.Context, sid session.SessionID, actor event.Actor) {
	a.mu.Lock()
	idx := -1
	for i, t := range a.stateLocked(sid).todos {
		if t.Status == "pending" {
			idx = i
			break
		}
	}
	a.mu.Unlock()
	if idx >= 0 {
		a.markTodoActive(ctx, sid, actor, idx)
	}
}

// finalizeTodos resolves the plan when a top-level turn ends: on genuine completion
// every unfinished todo becomes completed (the council judged the task satisfied);
// otherwise — abort, loop-guard, max-steps, error, panic — they become cancelled (the
// panel shows what was left undone). Best-effort: a no-op when nothing changed, and
// appendFact errors are ignored (the store may be closing on shutdown).
func (a *App) finalizeTodos(ctx context.Context, sid session.SessionID, finished bool) {
	target := "cancelled"
	if finished {
		target = "completed"
	}
	a.mu.Lock()
	td := a.stateLocked(sid).todos
	if len(td) == 0 {
		a.mu.Unlock()
		return
	}
	cp := append([]session.Todo(nil), td...)
	changed := false
	for i := range cp {
		// Never resurrect a cancelled step — mirror advanceTo. On a genuine finish this keeps a
		// step that was explicitly cancelled mid-turn from being relabelled "completed" (currently
		// unreachable, since only this function sets "cancelled", but the guard keeps the two paths
		// consistent so a future mid-turn cancel can't be laundered into a false completion).
		if cp[i].Status != "completed" && cp[i].Status != "cancelled" {
			cp[i].Status = target
			changed = true
		}
	}
	if !changed {
		a.mu.Unlock()
		return
	}
	a.stateLocked(sid).todos = cp
	a.mu.Unlock()
	d, _ := json.Marshal(event.TodosChangedData{Todos: cp})
	_ = a.appendFact(ctx, sid, event.TypeTodosChanged, event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
}

// todosEqual reports whether two plans are identical, so putTodos skips no-op writes.
func todosEqual(a, b []session.Todo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// noteForTurn records, verbatim, one thing the agent asked to be reminded of before this turn ends
// (remember{scope:"turn"}). magi stores the string and nothing else: it does not read it, rank it,
// or decide when it is relevant — the agent said it mattered, and that is the whole of what magi
// knows about it.
//
// The push is the point. The session already has four ways to keep something (remember,
// recall_memory, recall_context, todowrite) and across seven graded tasks the first three were
// called zero times, because each of them requires the agent to think of asking. Measured on those
// runs: one agent worked out a field mapping and spent the next fifty minutes deriving it again;
// another proved a pointer bug with a standalone program and cycled for sixty-five minutes without
// landing the fix. Nothing was missing from the record — the conclusion was, and only the agent
// knew it was a conclusion.
func (a *App) noteForTurn(sid session.SessionID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("the note was empty, so there is nothing to hand back")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	for _, n := range st.turnNotes {
		if n == text {
			return nil // the same note twice is one note, and the first one is already queued
		}
	}
	// Bounded — and the bound is REPORTED. Dropping the note here and answering "noted" tells the
	// agent it has a reminder waiting that will never arrive, which is worse than no note at all:
	// it stops writing the fact down anywhere else.
	if len(st.turnNotes) >= turnNotesCap {
		return fmt.Errorf("this turn already has %d notes queued, which is the limit — this one was "+
			"NOT kept and will not be handed back; put it in your own reply, or save it with "+
			"scope \"project\" so it outlives the turn", turnNotesCap)
	}
	st.turnNotes = append(st.turnNotes, text)
	return nil
}

// turnNotesCap bounds what one turn can queue for itself. High enough that a working agent never
// meets it, low enough that a spinning one cannot fill the finish seam with its own text.
const turnNotesCap = 20

// turnNotesBlock renders the agent's own notes for the finish seams, or "" when it left none.
// Verbatim and in the order they were written — magi is a courier here, not a reader.
func (a *App) turnNotesBlock(sid session.SessionID) string {
	a.mu.Lock()
	notes := append([]string(nil), a.stateLocked(sid).turnNotes...)
	a.mu.Unlock()
	if len(notes) == 0 {
		return ""
	}
	return "── WHAT YOU ASKED TO CHECK BEFORE DECLARING THIS FINISHED ──\n- " +
		strings.Join(notes, "\n- ")
}

// putLabels records what the agent says this session's work is about.
//
// Unconditional, unlike putTodos, which skips a write when the plan is unchanged. A plan is written
// on nearly every step and the guard is there to keep a log from filling with copies; labels are
// set once or twice in a session, so the guard would cost a comparison to save nothing — and a
// repeated write of the same set is harmless, since the last one wins on read.
func (a *App) putLabels(ctx context.Context, sid session.SessionID, actor event.Actor, ls []string) {
	d, err := json.Marshal(event.LabelsChangedData{Labels: ls})
	if err != nil {
		return
	}
	if err := a.appendFact(ctx, sid, event.TypeLabelsChanged, actor, d); err != nil {
		// Best-effort, and said so: a label that did not land costs a card its chip and nothing
		// else, and failing the tool call over it would make the agent retry a thing that is not
		// the work. The log is where a store that cannot be written to shows up.
		log.Printf("magi: recording labels: %v", err)
	}
}
