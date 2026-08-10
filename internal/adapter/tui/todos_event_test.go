package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A plan made after the viewer attached reaches the panel.
//
// In one process this is free: the engine writing the plan and the engine the panel asks are the
// same object. Attached to a daemon they are not — the plan is read from session state held in
// memory, filled once when a session is opened — so a plan made after attaching stayed invisible
// for as long as the viewer stayed attached. The panel hides itself when there is no plan, so what
// a person saw was a companion working through a plan it appeared not to have.
//
// The event is the only thing that crosses, which is why this is the seam.
func TestAPlanMadeAfterAttachingReachesThePanel(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 160, 48
	if m.hasPanel() {
		t.Fatal("precondition: the panel is up before there is anything in it")
	}

	m.applyEvent(ev(t, event.TypeTodosChanged, event.TodosChangedData{Todos: []session.Todo{
		{Content: "read the failing test", Status: "completed"},
		{Content: "fix the parser", Status: "in_progress"},
	}}))

	if !m.hasPanel() {
		t.Fatal("a plan arrived and the panel stayed hidden")
	}
	// Into the engine, not beside it: the panel, the transcript's step marks and /plan all ask the
	// engine, and a copy kept here would be a second answer for them to drift between.
	if got := m.app.Todos(m.sid); len(got) != 2 || got[1].Content != "fix the parser" {
		t.Fatalf("the engine's idea of the plan is %+v", got)
	}
	if box, _, _, ok := m.floatPanel(); !ok {
		t.Error("the panel has content and does not draw")
	} else if want := "fix the parser"; !strings.Contains(box, want) {
		t.Errorf("the panel does not show the plan:\n%s", box)
	}

	// A later plan replaces the earlier one outright — the record is the whole plan each time, and
	// merging would resurrect a step the agent decided against.
	m.applyEvent(ev(t, event.TypeTodosChanged, event.TodosChangedData{Todos: []session.Todo{
		{Content: "ship it", Status: "pending"},
	}}))
	if got := m.app.Todos(m.sid); len(got) != 1 || got[0].Content != "ship it" {
		t.Fatalf("the replacing plan left %+v", got)
	}
}
