package tui

import (
	"reflect"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// fakeTree is a static planTree for exercising activePlanPath without a live App.
type fakeTree struct {
	todos map[session.SessionID][]session.Todo
	kids  map[session.SessionID]map[int][]session.SessionID
}

func (f fakeTree) Todos(s session.SessionID) []session.Todo { return f.todos[s] }
func (f fakeTree) PlanChildren(s session.SessionID, i int) []session.SessionID {
	return f.kids[s][i]
}

func td(content, status string) session.Todo { return session.Todo{Content: content, Status: status} }

// A single-level plan reports top-level progress and the in-progress step as the sole crumb.
func TestActivePlanPathSingleLevel(t *testing.T) {
	ft := fakeTree{todos: map[session.SessionID][]session.Todo{
		"root": {td("Parse spec", "completed"), td("Wire resolver", "in_progress"), td("Tests", "pending")},
	}}
	done, total, crumbs := activePlanPath(ft, "root")
	if done != 1 || total != 3 {
		t.Fatalf("progress = %d/%d, want 1/3", done, total)
	}
	if !reflect.DeepEqual(crumbs, []string{"Wire resolver"}) {
		t.Fatalf("crumbs = %v, want [Wire resolver]", crumbs)
	}
}

// A refine child's active sub-step extends the breadcrumb one level deeper — the child's
// todos live in its own session, joined by the PlanChildren edge on the parent step.
func TestActivePlanPathDescendsIntoRefineChild(t *testing.T) {
	ft := fakeTree{
		todos: map[session.SessionID][]session.Todo{
			"root":  {td("Parse spec", "completed"), td("Wire resolver", "in_progress"), td("Tests", "pending")},
			"child": {td("add cache layer", "in_progress"), td("invalidate on write", "pending")},
		},
		kids: map[session.SessionID]map[int][]session.SessionID{
			"root": {1: {"child"}}, // step 1 (Wire resolver) spawned the refine child
		},
	}
	done, total, crumbs := activePlanPath(ft, "root")
	if done != 1 || total != 3 {
		t.Fatalf("progress = %d/%d, want 1/3 (top-level only)", done, total)
	}
	if !reflect.DeepEqual(crumbs, []string{"Wire resolver", "add cache layer"}) {
		t.Fatalf("crumbs = %v, want [Wire resolver, add cache layer]", crumbs)
	}
}

// With no step in progress (fresh or fully done) there is no active leaf to surface.
func TestActivePlanPathNoActiveStep(t *testing.T) {
	ft := fakeTree{todos: map[session.SessionID][]session.Todo{
		"root": {td("a", "completed"), td("b", "completed")},
	}}
	done, total, crumbs := activePlanPath(ft, "root")
	if done != 2 || total != 2 {
		t.Fatalf("progress = %d/%d, want 2/2", done, total)
	}
	if len(crumbs) != 0 {
		t.Fatalf("crumbs = %v, want empty", crumbs)
	}
}
