package main

import (
	"net/http"
)

// What is in one companion's head, and what was folded away to make room.
//
// Its own endpoint rather than a column on the fleet list, because the answer costs a full replay
// of the session's log — the same 12ms the fleet cache exists to avoid paying per row per poll —
// and it is a thing a person reads about ONE companion while looking at it, not a thing they scan
// down a column of.
func (s *server) context(w http.ResponseWriter, r *http.Request) {
	// A companion on another console is asked THERE: the log is on that machine and this process
	// has no copy of it.
	if s.forwarded(w, r, s.proxy) {
		return
	}
	sid, ok := s.session(w, r)
	if !ok {
		return
	}
	st, err := s.reader.ContextStateOf(r.Context(), sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, "context state", st)
}
