package tui

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A model.changed event refreshes the cached header chip (m.model) and, when the
// routing editor is open, its session row — so a runtime model change from any
// source (plugin set_model, /route, reload_config) is reflected without a stale copy.
func TestModelChangedEventRefreshesDisplay(t *testing.T) {
	m := routedTestModel(t)
	if m.model != "base" {
		t.Fatalf("precondition: header model = %q, want base", m.model)
	}

	m.applyEvent(ev(t, event.TypeModelChanged, event.ModelChangedData{Model: "swapped"}))
	if m.model != "swapped" {
		t.Fatalf("header model after event = %q, want swapped", m.model)
	}

	// With the routing editor open, the session row (routeList[0]) tracks it too.
	m.openRouteEditor()
	m.applyEvent(ev(t, event.TypeModelChanged, event.ModelChangedData{Model: "again"}))
	if m.model != "again" {
		t.Fatalf("header model = %q, want again", m.model)
	}
	if got := m.routeList[0].value; got != "again" {
		t.Fatalf("route session row = %q, want again", got)
	}

	// An empty payload is ignored (no clobber to blank).
	m.applyEvent(ev(t, event.TypeModelChanged, event.ModelChangedData{Model: ""}))
	if m.model != "again" {
		t.Fatalf("empty model.changed must not clobber, got %q", m.model)
	}
}
