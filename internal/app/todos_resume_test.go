package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A resumed session gets its plan back from the log.
//
// todos.changed was written on every plan change and read by nothing. Todos() answers from
// sessionState, which a process that never ran the session has never filled — so reopening a
// session showed an empty plan panel, and prompt.go left the plan block out of the agent's own
// context, while every step sat in the log.
func TestAResumedSessionGetsItsPlanBack(t *testing.T) {
	a, sid := newTodoLogApp(t,
		[]session.Todo{{Content: "write the parser", Status: "completed"}},
		[]session.Todo{
			{Content: "write the parser", Status: "completed"},
			{Content: "wire it up", Status: "in_progress"},
		})

	if got := a.Todos(sid); len(got) != 0 {
		t.Fatalf("nothing has read the log yet, so the plan must be empty: %+v", got)
	}
	if _, _, err := a.SessionState(context.Background(), sid); err != nil {
		t.Fatalf("SessionState: %v", err)
	}

	got := a.Todos(sid)
	if len(got) != 2 {
		t.Fatalf("the resumed plan has %d steps, want the 2 the last fact recorded: %+v", len(got), got)
	}
	// The LAST fact is the whole plan, not a delta — an earlier one replayed over it would
	// lose the step added since.
	if got[1].Content != "wire it up" || got[1].Status != "in_progress" {
		t.Errorf("the resumed plan is not the latest one: %+v", got)
	}
	if got[0].Status != "completed" {
		t.Errorf("a finished step came back unfinished: %+v", got[0])
	}
}

// Switching away from a running session and back must not roll its progress backwards: the
// in-memory plan is the newer truth, and the log's last fact can lag a step behind it.
func TestResumeDoesNotOverwriteALivePlan(t *testing.T) {
	a, sid := newTodoLogApp(t, []session.Todo{{Content: "stale step", Status: "pending"}})

	live := []session.Todo{{Content: "live step", Status: "in_progress"}}
	a.mu.Lock()
	a.stateLocked(sid).todos = live
	a.mu.Unlock()

	if _, _, err := a.SessionState(context.Background(), sid); err != nil {
		t.Fatalf("SessionState: %v", err)
	}
	if got := a.Todos(sid); len(got) != 1 || got[0].Content != "live step" {
		t.Errorf("resume clobbered the live plan with the log's older one: %+v", got)
	}
}

// newTodoLogApp builds an app whose store holds a session with one todos.changed fact per
// argument, in order.
func newTodoLogApp(t *testing.T, plans ...[]session.Todo) (*App, session.SessionID) {
	t.Helper()
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(store, &usageLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(),
		Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	for _, p := range plans {
		d, _ := json.Marshal(event.TodosChangedData{Todos: p})
		if err := a.appendFact(ctx, sid, event.TypeTodosChanged, actor, d); err != nil {
			t.Fatalf("appendFact: %v", err)
		}
	}
	// Drop the in-memory state so the app looks like a process that never ran this session.
	a.mu.Lock()
	delete(a.states, sid)
	a.mu.Unlock()
	return a, sid
}
