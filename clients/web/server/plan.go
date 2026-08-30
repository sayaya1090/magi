package main

import (
	"net/http"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The plan a companion is working through.
//
// The fleet row carries the count — "3/7" is what tells a person glancing twice in ten minutes
// whether anything is moving — and this is the list behind it, for the one companion whose page is
// open. Split for the reason the context reading is split: the count comes free with a derivation
// the list already pays for, and the items are a second read nobody should pay per row per poll.
func (s *server) plan(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	sid, ok := s.session(w, r)
	if !ok {
		return
	}
	todos, err := s.reader.PlanOf(r.Context(), sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if todos == nil {
		// An empty list, never null: the page iterates what it gets, and a companion that has not
		// made a plan is the ordinary case rather than an error.
		todos = []session.Todo{}
	}
	writeJSON(w, "plan", todos)
}
