package app

import (
	"context"
	"encoding/json"

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

// completedStepCount returns how many of the session's plan steps are currently marked completed.
// It is the convergence signal noteReplan re-baselines against: if this does not climb across
// repeated replans, the re-decomposition is finishing nothing.
func (a *App) completedStepCount(sid session.SessionID) int {
	n := 0
	for _, t := range a.Todos(sid) {
		if t.Status == "completed" {
			n++
		}
	}
	return n
}

// completeThrough marks every plan step up to and including index i completed. A
// procedure runs top-to-bottom, so finishing step i means the steps before it are done
// too — this both checks off a step the planner ran in pre-flight AND back-fills any
// earlier step the fan-out subsumed, so the panel reads as sequential progress instead
// of a lone middle ✓ (which is what made an aborted run look like ✗✓✗). Copy-on-write
// under one lock — Todos() hands the slice to the TUI lock-free — then one fact is
// emitted outside the lock, only if something changed.
func (a *App) completeThrough(ctx context.Context, sid session.SessionID, actor event.Actor, i int) {
	a.mu.Lock()
	td := a.stateLocked(sid).todos
	if i < 0 || i >= len(td) {
		a.mu.Unlock()
		return
	}
	cp := append([]session.Todo(nil), td...)
	changed := false
	var newlyDone []int
	for j := 0; j <= i; j++ {
		if cp[j].Status != "completed" {
			cp[j].Status = "completed"
			changed = true
			newlyDone = append(newlyDone, j)
		}
	}
	if !changed {
		a.mu.Unlock()
		return
	}
	a.stateLocked(sid).todos = cp
	a.mu.Unlock()
	d, _ := json.Marshal(event.TodosChangedData{Todos: cp})
	_ = a.appendFact(ctx, sid, event.TypeTodosChanged, actor, d)
	// Per-step completion checks: the moment a step lands, run and record ITS deliverable checks so
	// the panel fills incrementally on every path (delegate/scout/refine), instead of the terminal
	// gate recording them all at once. Idempotent — a check the delegate step gate already passed is
	// skipped. Runs outside the lock (shell commands); a no-op when step-verify is off or no checks.
	for _, j := range newlyDone {
		a.recordStepChecks(ctx, sid, j)
	}
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

// advanceTo records that the procedure has MOVED ON to step i: every earlier step is
// completed and step i goes in_progress (◐). Doing this when a step STARTS — rather
// than back-filling earlier steps only when the next fan-out FINISHES — is what removes
// the one-beat lag where step 1 stayed pending until step 2 completed (a solo step
// before a fan-out has no signal of its own). One fact, only if something changed.
func (a *App) advanceTo(ctx context.Context, sid session.SessionID, actor event.Actor, i int) {
	a.mu.Lock()
	td := a.stateLocked(sid).todos
	if i < 0 || i >= len(td) {
		a.mu.Unlock()
		return
	}
	cp := append([]session.Todo(nil), td...)
	changed := false
	for j := 0; j < i; j++ {
		if cp[j].Status != "completed" {
			cp[j].Status = "completed"
			changed = true
		}
	}
	if cp[i].Status == "pending" {
		cp[i].Status = "in_progress"
		changed = true
	}
	if !changed {
		a.mu.Unlock()
		return
	}
	a.stateLocked(sid).todos = cp
	a.mu.Unlock()
	d, _ := json.Marshal(event.TodosChangedData{Todos: cp})
	_ = a.appendFact(ctx, sid, event.TypeTodosChanged, actor, d)
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
		if cp[i].Status != "completed" {
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
