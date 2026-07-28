package tui

import (
	"reflect"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// fakeTree is a static plan for exercising activePlanPath without a live App.
type fakeTree struct {
	todos map[session.SessionID][]session.Todo
}

func (f fakeTree) Todos(s session.SessionID) []session.Todo { return f.todos[s] }

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
