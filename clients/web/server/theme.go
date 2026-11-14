package main

import (
	"net/http"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
)

// theme is the colour palette the aimed daemon resolved from the person's [theme] config, light and
// dark, role name to hex. The page hands it to the diagram viewer so a diagram is painted with the
// person's palette rather than the copy compiled into the page. Read from the daemon because only
// that process read the config; an older daemon that cannot say answers null and the viewer keeps
// the host's own roles.
func (s *server) theme(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET")
		http.Error(w, "the theme is only read here", http.StatusMethodNotAllowed)
		return
	}
	var th *daemon.Theme
	err := s.withClient(r, func(cl *daemon.Client, sid session.SessionID) error {
		st, err := cl.Status(string(sid))
		th = st.Theme
		return err
	})
	if err != nil {
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	writeJSON(w, "theme", th)
}
