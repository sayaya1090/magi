package tui

import "github.com/sayaya1090/magi/internal/core/session"

// planTree is the read-only view activePlanPath needs: a session's plan todos and
// the child sessions spawned for a given plan step. *app.App already satisfies it
// (the panel uses the same two methods to render the nested plan tree), so a fake
// can exercise activePlanPath without a live App.
type planTree interface {
	Todos(session.SessionID) []session.Todo
	PlanChildren(session.SessionID, int) []session.SessionID
}

// activePlanPath walks the live plan tree from sid down its in-progress branch. It
// returns the TOP-LEVEL progress (completed/total) plus the title of the in-progress
// step at each level — so a refine/delegate child's active sub-step surfaces as a
// breadcrumb even though its todos live in the child session (the plan tree keeps
// each node's status in its own session; PlanChildren supplies the edges). crumbs is
// empty when no step is in progress. Depth is bounded by planTreeMaxDepth, the same
// guard the panel renderer uses, so a pathological chain can't loop unbounded.
func activePlanPath(pt planTree, sid session.SessionID) (done, total int, crumbs []string) {
	todos := pt.Todos(sid)
	total = len(todos)
	for _, t := range todos {
		if t.Status == "completed" {
			done++
		}
	}
	cur, curTodos := sid, todos
	for depth := 0; depth <= planTreeMaxDepth; depth++ {
		idx := -1
		for i, t := range curTodos {
			if t.Status == "in_progress" {
				idx = i
				break
			}
		}
		if idx < 0 {
			break // this level has no running step — the branch ends here
		}
		crumbs = append(crumbs, curTodos[idx].Content)
		// Descend into the step's latest child (the current attempt carries the live
		// sub-plan); stop when the step spawned no child or the child has no plan yet.
		kids := pt.PlanChildren(cur, idx)
		if len(kids) == 0 {
			break
		}
		child := kids[len(kids)-1]
		childTodos := pt.Todos(child)
		if len(childTodos) == 0 {
			break
		}
		cur, curTodos = child, childTodos
	}
	return done, total, crumbs
}
