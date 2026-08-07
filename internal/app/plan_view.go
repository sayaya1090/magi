package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The plan a companion is working through, read out of its log.
//
// Todos() answers this for the process that is running the turn — it reads the session state held
// in memory — which is exactly the wrong seam for anybody watching from outside. A console is a
// different process, so it would always see an empty plan, and a plan that is empty because nobody
// asked the right question looks identical to one that was never made.
//
// The log already carries it: todos.changed records the WHOLE plan each time, so the last one is
// the current one. Which means this answers for a companion that has stopped, for a session resumed
// from another machine, and for last week — none of which an in-memory read can do.
//
// The comment on TodosChangedData has said "a reader could rebuild the latest state from the log if
// needed" since it was written. This is that reader.
func (a *App) PlanOf(ctx context.Context, sid session.SessionID) ([]session.Todo, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil, err
	}
	return planFrom(evs), nil
}

func planFrom(evs []event.Event) []session.Todo {
	var out []session.Todo
	for _, e := range evs {
		if e.Type != event.TypeTodosChanged {
			continue
		}
		var d event.TodosChangedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		// The whole plan, each time — so the last record wins outright rather than merging. A merge
		// would resurrect an item the agent deliberately dropped.
		out = d.Todos
	}
	return out
}

// PlanProgress counts what is done. Separate from the list because the fleet view wants the count
// on every row and the detail wants the items, and reading the log twice for one answer is the
// thing the fleet cache exists to avoid.
func PlanProgress(todos []session.Todo) (done, total int) {
	for _, t := range todos {
		total++
		if t.Status == "completed" || t.Status == "done" {
			done++
		}
	}
	return done, total
}
