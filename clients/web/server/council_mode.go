package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Council state and changes are tied to the session displayed when the control was used.
func (s *server) councilMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	wanted := r.FormValue("session")
	if wanted == "" {
		http.Error(w, "no session named", http.StatusBadRequest)
		return
	}
	enabled := false
	if r.Method == http.MethodPost {
		switch r.FormValue("enabled") {
		case "true":
			enabled = true
		case "false":
		default:
			http.Error(w, "enabled must be true or false", http.StatusBadRequest)
			return
		}
	}
	var state bool
	err := s.withClient(r, func(cl *daemon.Client, sid session.SessionID) error {
		if string(sid) != wanted {
			return daemon.Refused{Why: "the active chat has changed; refresh before changing council"}
		}
		if r.Method == http.MethodPost {
			return cl.SetCouncil(wanted, enabled)
		}
		status, err := cl.Status(wanted)
		state = status.Council
		return err
	})
	if err != nil {
		http.Error(w, fmt.Sprint(err), daemonSaysNo(err))
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"enabled": state, "session": wanted}); err != nil {
		log.Printf("write council state: %v", err)
	}
}
