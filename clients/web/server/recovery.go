package main

import (
	"net/http"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// verdicts are the words a person may say about a call a restart left uncertain. Checked here so a
// typo is a 400 in the browser, not a round trip to a daemon that would refuse it anyway.
var verdicts = map[string]bool{
	string(app.VerdictRan): true, string(app.VerdictNotRan): true,
	string(app.VerdictRerunOK): true, string(app.VerdictDeferred): true,
}

// recovery is what the last stop left in a companion's conversation, and the person's word on it.
//
// Read from the DAEMON, not from this process's own reader: a recovery the daemon could not finish
// is known only to the daemon (it could not write it down — that is what failed), and a verdict is
// written to the log the daemon owns. Every answer is the state as the daemon reads it afterwards,
// so the banner redraws from the truth rather than from what the page sent.
//
//	GET               the state
//	POST retry=1      look at the conversation again
//	POST call, verdict[, note]   ran | not_ran | rerun_ok | deferred
func (s *server) recovery(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	var st app.RecoveryState
	var do func(*daemon.Client, session.SessionID) error
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		do = func(cl *daemon.Client, sid session.SessionID) (err error) {
			st, err = cl.RecoveryState(string(sid))
			return err
		}
	case http.MethodPost:
		if r.FormValue("retry") != "" {
			do = func(cl *daemon.Client, sid session.SessionID) (err error) {
				st, err = cl.RetryRecovery(string(sid))
				return err
			}
			break
		}
		call := strings.TrimSpace(r.FormValue("call"))
		verdict := strings.TrimSpace(r.FormValue("verdict"))
		if call == "" || verdict == "" {
			http.Error(w, "say which call and what happened to it", http.StatusBadRequest)
			return
		}
		if !verdicts[verdict] {
			http.Error(w, "say what happened: ran, not_ran, rerun_ok or deferred", http.StatusBadRequest)
			return
		}
		note := strings.TrimSpace(r.FormValue("note"))
		do = func(cl *daemon.Client, sid session.SessionID) (err error) {
			st, err = cl.ResolveCall(string(sid), call, verdict, note)
			return err
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	if err := s.withClient(r, do); err != nil {
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	writeJSON(w, "recovery state", st)
}
