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
	a, sid := newTodoApp(t, Config{})
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

// todosEqual underpins putTodos's no-op dedup: true only for same-length plans with identical
// Content AND Status at every index. session.Todo is all-string (comparable), so the struct !=
// is safe; this locks the predicate directly (nil and empty both being length 0 → equal).
func TestTodosEqual(t *testing.T) {
	base := []session.Todo{{Content: "a", Status: "pending"}, {Content: "b", Status: "done"}}
	if !todosEqual(base, []session.Todo{{Content: "a", Status: "pending"}, {Content: "b", Status: "done"}}) {
		t.Error("identical plans must be equal")
	}
	if todosEqual(base, base[:1]) {
		t.Error("different lengths must not be equal")
	}
	if todosEqual(base, []session.Todo{{Content: "a", Status: "completed"}, {Content: "b", Status: "done"}}) {
		t.Error("a status difference must not be equal")
	}
	if todosEqual(base, []session.Todo{{Content: "X", Status: "pending"}, {Content: "b", Status: "done"}}) {
		t.Error("a content difference must not be equal")
	}
	if !todosEqual(nil, []session.Todo{}) {
		t.Error("nil and empty are both length 0 → equal")
	}
}

// markTodoActive shows a running step as in_progress (◐), but only starts a pending
// step — it never moves a completed/cancelled one back. markFirstPendingActive picks
// the first still-pending step (so it skips ones pre-flight already checked off).
func TestMarkTodoActive(t *testing.T) {
	a, sid := newTodoApp(t, Config{})
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
	a, sid := newTodoApp(t, Config{})
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
	a, sid := newTodoApp(t, Config{})
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

// A step cancelled mid-turn must NOT be relabelled "completed" on a genuine finish — cancelled is
// never resurrected; pending/in_progress still complete.
func TestFinalizeTodosPreservesCancelledOnFinish(t *testing.T) {
	a, sid := newTodoApp(t, Config{})
	a.putTodos(context.Background(), sid, event.Actor{Kind: event.ActorAgent, ID: "p"},
		[]session.Todo{{Content: "done", Status: "completed"}, {Content: "cx", Status: "cancelled"}, {Content: "p", Status: "pending"}})

	a.finalizeTodos(context.Background(), sid, true)
	got := a.Todos(sid)
	if got[1].Status != "cancelled" {
		t.Errorf("a cancelled step must stay cancelled on finish, not be relabelled completed, got %q", got[1].Status)
	}
	if got[0].Status != "completed" || got[2].Status != "completed" {
		t.Errorf("completed/pending steps should be completed on finish, got %q / %q", got[0].Status, got[2].Status)
	}
}

// Nothing to finalize → no event (empty plan, or already all-completed).
func TestFinalizeTodosNoOp(t *testing.T) {
	a, sid := newTodoApp(t, Config{})
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

// formatTodos maps each known status to its mark, joins with newlines, renders an empty plan as "",
// and falls back to the pending "[ ]" mark for an unknown/blank status — so a future status value the
// map doesn't cover degrades to a neutral box rather than a blank line.
func TestFormatTodosMappingAndFallback(t *testing.T) {
	if got := formatTodos(nil); got != "" {
		t.Errorf("empty plan must render as \"\", got %q", got)
	}
	got := formatTodos([]session.Todo{
		{Content: "a", Status: "completed"},
		{Content: "b", Status: "in_progress"},
		{Content: "c", Status: "pending"},
		{Content: "d", Status: "somethingnew"}, // unknown → falls back to the pending box
		{Content: "e", Status: ""},             // blank → same fallback
	})
	want := "[x] a\n[~] b\n[ ] c\n[ ] d\n[ ] e"
	if got != want {
		t.Errorf("formatTodos mapping/fallback:\n got %q\nwant %q", got, want)
	}
}
