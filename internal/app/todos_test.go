package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func todosChangedCount(t *testing.T, a *App, sid session.SessionID) int {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return countType(evs, event.TypeTodosChanged)
}

// putTodos records a TodosChanged fact only when the plan actually changes, so the
// progression is logged/replayable without spamming no-op events.
func TestPutTodosEmitsFactAndDedup(t *testing.T) {
	a, sid := newPlannerApp(t, Config{})
	actor := event.Actor{Kind: event.ActorAgent, ID: "planner"}
	td := []session.Todo{{Content: "a", Status: "pending"}, {Content: "b", Status: "pending"}}

	a.putTodos(context.Background(), sid, actor, td)
	if got := a.Todos(sid); len(got) != 2 || got[0].Content != "a" {
		t.Fatalf("Todos not stored: %+v", got)
	}
	if n := todosChangedCount(t, a, sid); n != 1 {
		t.Fatalf("want 1 TodosChanged, got %d", n)
	}
	// Same value → no new event.
	a.putTodos(context.Background(), sid, actor, []session.Todo{{Content: "a", Status: "pending"}, {Content: "b", Status: "pending"}})
	if n := todosChangedCount(t, a, sid); n != 1 {
		t.Errorf("identical plan should emit no event, got %d", n)
	}
	// Changed value → new event.
	a.putTodos(context.Background(), sid, actor, []session.Todo{{Content: "a", Status: "completed"}, {Content: "b", Status: "pending"}})
	if n := todosChangedCount(t, a, sid); n != 2 {
		t.Errorf("changed plan should emit an event, got %d", n)
	}
}

// completeThrough checks off a step AND every earlier step (the procedure is
// sequential, so finishing step i implies the ones before it). It records exactly one
// fact; a repeat call with nothing new to complete is a no-op.
func TestCompleteThrough(t *testing.T) {
	a, sid := newPlannerApp(t, Config{})
	actor := event.Actor{Kind: event.ActorAgent, ID: "p"}
	a.putTodos(context.Background(), sid, actor,
		[]session.Todo{{Content: "a", Status: "pending"}, {Content: "b", Status: "pending"}, {Content: "c", Status: "pending"}})
	base := todosChangedCount(t, a, sid) // 1 (the seed)

	// Completing the middle step (index 1) back-fills index 0 — no lone middle ✓.
	a.completeThrough(context.Background(), sid, actor, 1)
	got := a.Todos(sid)
	if got[0].Status != "completed" || got[1].Status != "completed" {
		t.Errorf("steps through index 1 should be completed, got %q / %q", got[0].Status, got[1].Status)
	}
	if got[2].Status != "pending" {
		t.Errorf("later step should stay pending, got %q", got[2].Status)
	}
	if n := todosChangedCount(t, a, sid); n != base+1 {
		t.Errorf("completeThrough should emit one fact, got %d (base %d)", n, base)
	}
	a.completeThrough(context.Background(), sid, actor, 1) // nothing new → no-op
	if n := todosChangedCount(t, a, sid); n != base+1 {
		t.Errorf("re-completing should emit nothing, got %d", n)
	}
}

// markTodoActive shows a running step as in_progress (◐), but only starts a pending
// step — it never moves a completed/cancelled one back. markFirstPendingActive picks
// the first still-pending step (so it skips ones pre-flight already checked off).
func TestMarkTodoActive(t *testing.T) {
	a, sid := newPlannerApp(t, Config{})
	actor := event.Actor{Kind: event.ActorAgent, ID: "p"}
	a.putTodos(context.Background(), sid, actor,
		[]session.Todo{{Content: "a", Status: "completed"}, {Content: "b", Status: "pending"}, {Content: "c", Status: "pending"}})

	// Won't restart a completed step.
	a.markTodoActive(context.Background(), sid, actor, 0)
	if a.Todos(sid)[0].Status != "completed" {
		t.Errorf("completed step must not be reactivated, got %q", a.Todos(sid)[0].Status)
	}
	// First pending step (index 1) goes in_progress; later pending stays pending.
	a.markFirstPendingActive(context.Background(), sid, actor)
	got := a.Todos(sid)
	if got[1].Status != "in_progress" {
		t.Errorf("first pending should be in_progress, got %q", got[1].Status)
	}
	if got[2].Status != "pending" {
		t.Errorf("later pending should stay pending, got %q", got[2].Status)
	}
	// A degraded fan-out reverts ◐ back to pending (no stuck in_progress); it won't
	// touch a step that isn't in_progress.
	a.setTodoStatusIf(context.Background(), sid, actor, 1, "in_progress", "pending")
	if a.Todos(sid)[1].Status != "pending" {
		t.Errorf("revert should return step to pending, got %q", a.Todos(sid)[1].Status)
	}
	a.markFirstPendingActive(context.Background(), sid, actor) // re-activate for the finalize check
	if a.Todos(sid)[1].Status != "in_progress" {
		t.Fatal("re-activate failed")
	}

	// in_progress is resolved by finalize like any unfinished step.
	a.finalizeTodos(context.Background(), sid, false)
	if s := a.Todos(sid); s[1].Status != "cancelled" || s[2].Status != "cancelled" {
		t.Errorf("unfinished (in_progress/pending) should cancel, got %q / %q", s[1].Status, s[2].Status)
	}
}

// On a genuine finish every unfinished todo becomes completed (the council judged the
// task done), and exactly one fact records it.
func TestFinalizeTodosCompleted(t *testing.T) {
	a, sid := newPlannerApp(t, Config{})
	a.putTodos(context.Background(), sid, event.Actor{Kind: event.ActorAgent, ID: "p"},
		[]session.Todo{{Content: "a", Status: "completed"}, {Content: "b", Status: "in_progress"}, {Content: "c", Status: "pending"}})
	base := todosChangedCount(t, a, sid)

	a.finalizeTodos(context.Background(), sid, true)
	for i, tdo := range a.Todos(sid) {
		if tdo.Status != "completed" {
			t.Errorf("todo %d = %q, want completed", i, tdo.Status)
		}
	}
	if n := todosChangedCount(t, a, sid); n != base+1 {
		t.Errorf("finalize should emit one event, got %d (base %d)", n, base)
	}
}

// On an abort/incomplete stop, unfinished todos become cancelled (✗) while already
// completed ones are preserved.
func TestFinalizeTodosCancelled(t *testing.T) {
	a, sid := newPlannerApp(t, Config{})
	a.putTodos(context.Background(), sid, event.Actor{Kind: event.ActorAgent, ID: "p"},
		[]session.Todo{{Content: "done", Status: "completed"}, {Content: "mid", Status: "in_progress"}, {Content: "todo", Status: "pending"}})

	a.finalizeTodos(context.Background(), sid, false)
	got := a.Todos(sid)
	if got[0].Status != "completed" {
		t.Errorf("completed todo should be preserved, got %q", got[0].Status)
	}
	if got[1].Status != "cancelled" || got[2].Status != "cancelled" {
		t.Errorf("unfinished todos should be cancelled, got %q / %q", got[1].Status, got[2].Status)
	}
}

// Nothing to finalize → no event (empty plan, or already all-completed).
func TestFinalizeTodosNoOp(t *testing.T) {
	a, sid := newPlannerApp(t, Config{})
	a.finalizeTodos(context.Background(), sid, true) // no plan
	if n := todosChangedCount(t, a, sid); n != 0 {
		t.Errorf("empty plan should emit nothing, got %d", n)
	}

	a.putTodos(context.Background(), sid, event.Actor{Kind: event.ActorAgent, ID: "p"},
		[]session.Todo{{Content: "a", Status: "completed"}})
	base := todosChangedCount(t, a, sid)
	a.finalizeTodos(context.Background(), sid, true) // already complete
	if n := todosChangedCount(t, a, sid); n != base {
		t.Errorf("all-completed plan should emit nothing on finalize, got %d (base %d)", n, base)
	}
}

// formatTodos renders the cancelled status for the model-facing plan.
func TestFormatTodosCancelled(t *testing.T) {
	s := formatTodos([]session.Todo{{Content: "x", Status: "cancelled"}})
	if want := "[✗] x"; s != want {
		t.Errorf("formatTodos = %q, want %q", s, want)
	}
}
