package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
)

// What a companion handed to the others, and what came back.
//
// There is no reply channel by design: a companion answers in its own transcript, and the asker
// reads it. That is cheap and honest — no message queue to lose things, no callback that arrives
// mid-turn and derails the asker — but it leaves a person clicking through five pages to find out
// whether the work they dispatched is done. This is that walk, done once.
//
// Its own endpoint rather than part of /fleet: it replays the receivers' transcripts, which is the
// cost the fleet cache exists to keep off the poll.
func (s *server) handoffs(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	// Named by the companion whose page this is, when there is one. The label carries the ASKER's
	// declared name, which is what the resolver knows them by too.
	from := r.URL.Query().Get("from")
	if from == "" && r.URL.Query().Get("d") != "" {
		if in, err := s.target(r); err == nil {
			for _, a := range s.published(r) {
				if a.Socket == in.Socket {
					from = a.Name
					break
				}
			}
		}
	}
	list, err := fleet.Handoffs(r.Context(), s.reader, s.cfgDir, from, &s.fleetCache)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(list); err != nil {
		log.Printf("magi-web: writing the handoffs: %v", err)
	}
}

// published is this console's own companions, as the fleet sees them.
func (s *server) published(r *http.Request) []fleet.Agent {
	list, err := fleet.ListCached(r.Context(), s.reader, s.cfgDir, s.here, &s.fleetCache)
	if err != nil {
		return nil
	}
	return list
}
