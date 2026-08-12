package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/auth"
)

// Addressing a companion by what it IS rather than by where it lives.
//
// Every other action here names a socket, because every other action starts from a row somebody
// clicked. This one starts from the work: "the design system needs a component spec" is addressed
// to whoever does design, and which machine that is on is exactly the thing the person asking
// should not have to know. That is the whole difference between a list of processes and a team.
//
// # Resolution, and why it refuses rather than picks
//
// A name is matched first and exactly — a name is chosen by whoever set the companion up, so it is
// the precise address. A role is matched loosely, because it is a sentence.
//
// Two matches are an error, never a choice. Picking one would send work to a companion the person
// did not mean and would do it silently; the failure is a list of who matched, so the next attempt
// names one. This is the one place where being unhelpful is the helpful behaviour: the cost of the
// wrong guess is a turn running in somebody else's workspace.
//
// # Where it dispatches to
//
// Through the same forwarding every other action uses: the resolved companion's socket, and its
// console when it is on another machine. Nothing new crosses the wire, and a companion two hops
// away is not addressable, which is deliberate — this console knows the consoles its operator
// listed and no others.
func (s *server) dispatch(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	to := strings.TrimSpace(r.FormValue("to"))
	text := strings.TrimSpace(r.FormValue("text"))
	if to == "" || text == "" {
		http.Error(w, "a dispatch needs somebody to do it and something to do", http.StatusBadRequest)
		return
	}

	local, err := fleet.ListCached(r.Context(), s.reader, s.cfgDir, s.here, &s.fleetCache)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	all := s.federated(r.Context(), local)
	found := fleet.Resolve(all, to)
	switch len(found) {
	case 0:
		http.Error(w, fmt.Sprintf("nobody here is called %q or does that. This team is: %s",
			to, fleet.Roster(all)), http.StatusNotFound)
		return
	case 1:
	default:
		http.Error(w, fmt.Sprintf("%q matches %s — name one of them, because sending work to the "+
			"wrong workspace is not something to guess at", to, fleet.Names(found)), http.StatusConflict)
		return
	}
	target := found[0]
	// The record's Agent is the socket in the URL, which for a dispatch is the companion doing the
	// asking. What was acted on is this one.
	noteSubject(r, target.Name)
	// Asked AGAIN, now that the subject is known. The gate checked the companion this request
	// NAMED in its query and this route acts on the one it just resolved from the body — so
	// somebody scoped to one companion could dispatch to another, and the front door had no way to
	// see it. The check belongs where the target stops being a guess.
	if !s.seen(r, target.Name, target.Peer) {
		http.Error(w, refusal(s.whoFrom(r), auth.Prompt), http.StatusForbidden)
		return
	}
	if target.State == fleet.Stopped {
		// Not an error worth refusing over — a stopped daemon may be started again and the prompt
		// would be waiting — but it cannot be delivered now, and saying "sent" would be a lie.
		http.Error(w, fmt.Sprintf("%s is not running, so there is nothing to hand the work to",
			target.Name), http.StatusConflict)
		return
	}

	// Re-addressed and forwarded through the ordinary submit path, so a dispatch and a person
	// typing into that companion's page reach the daemon by exactly one route.
	rr := r.Clone(r.Context())
	q := url.Values{"d": {target.Socket}}
	if target.Peer != "" {
		q.Set("p", target.Peer)
	}
	rr.URL.RawQuery = q.Encode()
	rr.Form, rr.PostForm = nil, nil
	rr.Body = formBody(r.PostForm)
	rr.ContentLength = -1
	s.submit(w, rr)
}

// formBody re-encodes a parsed form so the forwarded request carries the same fields.
func formBody(v url.Values) io.ReadCloser {
	return io.NopCloser(strings.NewReader(v.Encode()))
}
