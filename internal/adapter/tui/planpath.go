package tui

import "github.com/sayaya1090/magi/internal/core/session"

// planSteps is the read-only view activePlanPath needs — narrower than Engine on purpose, so a
// fake can exercise activePlanPath without standing up the whole boundary.
type planSteps interface {
	Todos(session.SessionID) []session.Todo
}

// activePlanPath reports the plan's progress (completed/total) and the title of the step now in
// progress, for the header. It used to walk DOWN a tree of child sessions, surfacing a delegated
// sub-step as a second breadcrumb; there are no child sessions now, so the path is one step deep.
// crumbs is empty when nothing is in progress.
func activePlanPath(pt planSteps, sid session.SessionID) (done, total int, crumbs []string) {
	todos := pt.Todos(sid)
	total = len(todos)
	for _, t := range todos {
		if t.Status == "completed" {
			done++
		}
	}
	for _, t := range todos {
		if t.Status == "in_progress" {
			crumbs = append(crumbs, t.Content)
			break
		}
	}
	return done, total, crumbs
}
