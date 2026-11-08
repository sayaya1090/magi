package main

import (
	"net/http"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// slots handles GET /slots (and /engram) for querying plugin UI slots,
// and POST /slots (and /engram) for executing slot actions.
func (s *server) slots(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var slots []port.PluginSlot
		err := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
			resp, err := cl.Knowledge(port.KnowledgeAction{})
			if err == nil && resp.OK {
				slots = resp.Slots
			}
			return err
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if slots == nil {
			slots = []port.PluginSlot{}
		}
		writeJSON(w, "slots", slots)
		return
	}

	if postOnly(w, r) {
		return
	}

	action := r.FormValue("action")
	plugin := r.FormValue("plugin")
	if action == "" {
		http.Error(w, "action is required", http.StatusBadRequest)
		return
	}

	var resp daemon.Response
	err := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		var kerr error
		resp, kerr = cl.Knowledge(port.KnowledgeAction{Name: plugin, Action: action})
		return kerr
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !resp.OK {
		http.Error(w, resp.Err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, "slot-action", resp)
}
