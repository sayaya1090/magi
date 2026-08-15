package main

import (
	"net/http"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
)

// update tells a same-machine companion to update itself to the latest release and restart onto it —
// the manual push from the agent list for a companion that is behind. It relays the daemon's own
// `update` method (download + commit-with-rollback + restart; see internal/update and the daemon).
//
// Same-machine only, three ways: it is behind refuseWhenShared (swapping the binary and restarting is
// a change to the machine, so only the operator does it, not anyone an authenticating proxy let in);
// it refuses a peer-scoped request (?p=) rather than forwarding it over ssh, because the whole point
// of the scope is that nothing here updates a companion on another machine; and the daemon's `update`
// method is not on the fleet-door allowlist, so a peer could not reach it even if this let one try.
//
// A successful update replies before the daemon drains to restart, so the reply arrives; the socket
// then goes for the sub-second the successor takes to rebind, which the page rides out on its next
// poll.
func (s *server) update(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.refuseWhenShared(w, "updating this machine's magi") {
		return
	}
	if r.URL.Query().Get("p") != "" {
		http.Error(w, "an update is same-machine only — run it on the machine that companion is on, "+
			"not across a peer", http.StatusForbidden)
		return
	}
	var out string
	if derr := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, uerr := cl.Update()
		out = said
		return uerr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	writeText(w, out)
}
