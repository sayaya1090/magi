package main

import (
	"net/http"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What the terminal answers with /tools and /loop, as screens rather than as transcript rows.
//
// The terminal prints these into the conversation because a terminal has nowhere else to put them.
// A console does: it already answers the same kind of question — how full the context is, what this
// companion has done — with a card and a screen. Copying the terminal's info ROWS here would be
// copying a constraint the browser does not have, and it would leave the transcript carrying two
// kinds of thing: what happened, and what somebody asked to look at.
//
// So the three gaps are filled where they belong. Which tools this companion has is a property of
// the process holding the run, so it comes over the socket. The loop map and the comparison with a
// fork's origin are readings of the log, so this process does them itself — the same division every
// other screen here follows.

// tools is the roster this companion is actually running with.
//
// Over the socket, and not from a list built here: the registry is assembled at startup from the
// config, the plugins that loaded and the MCP servers that answered, so a console that listed the
// built-ins would be describing a companion that does not exist. A daemon too old to answer says
// nothing rather than being guessed at.
func (s *server) tools(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	out := []string{}
	if err := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
		names, terr := cl.Tools()
		if terr != nil {
			return terr
		}
		out = names
		return nil
	}); err != nil {
		// Not an error page. A companion that cannot say is a companion whose roster this console
		// does not know, which is what an empty list means — and the screen says so in words.
		writeJSON(w, "tools", []string{})
		return
	}
	writeJSON(w, "tools", out)
}

// models is what this companion could be put on, and putting it on one.
//
// GET asks the daemon, which asks its own backend: the list depends on what that process is
// configured against and what that backend answers today, so a console listing from its own config
// would offer models this companion cannot reach. POST is the change, and it crosses for the reason
// every control does — done here it would set a field in a copy of the engine nobody is running.
func (s *server) models(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	if r.Method == http.MethodPost {
		name := strings.TrimSpace(r.FormValue("model"))
		if name == "" {
			http.Error(w, "no model named", http.StatusBadRequest)
			return
		}
		if err := s.withClient(r, func(cl *daemon.Client, sid session.SessionID) error {
			return cl.SetModel(sid, name)
		}); err != nil {
			http.Error(w, err.Error(), daemonSaysNo(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	out := []string{}
	if err := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
		names, merr := cl.Models()
		if merr != nil {
			return merr
		}
		out = names
		return nil
	}); err != nil {
		// An empty list, not an error page: the screen then shows the model it is on and does not
		// offer to change it, which is the truthful picture when nobody could say what else exists.
		writeJSON(w, "models", []string{})
		return
	}
	writeJSON(w, "models", out)
}

// loopShape is the map of a session's turns, and — when this session was forked from another — how
// it has diverged from where it came from.
type loopShape struct {
	Map string `json:"map"`
	// Origin is the session this one was forked from, and Diff is what changed since. Empty when
	// this session is nobody's fork, which is most of them: a diff against nothing would be the
	// whole transcript, dressed up as a comparison.
	Origin string `json:"origin,omitempty"`
	Diff   string `json:"diff,omitempty"`
}

func (s *server) loop(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// A session named by the page is checked against the ones this workspace owns, for the reason
	// every other read here is: an id in a URL must not become a path this process opens.
	sid := session.SessionID(in.Session)
	if want := r.URL.Query().Get("session"); want != "" {
		if !s.readable(r, in, want) {
			http.Error(w, "not this companion's to read", http.StatusNotFound)
			return
		}
		sid = session.SessionID(want)
	}
	shape := loopShape{}
	m, merr := s.reader.LoopMap(r.Context(), sid)
	if merr != nil {
		http.Error(w, merr.Error(), http.StatusInternalServerError)
		return
	}
	shape.Map = m
	// The fork's origin, from the store rather than from a flag the terminal happens to be holding:
	// /loopdiff in the terminal compares against whatever that session forked from this run, and a
	// console arriving later has no such memory. The log does.
	for _, meta := range s.sessionsOf(r, in.Workdir) {
		if meta.ID == sid && meta.Parent != "" {
			shape.Origin = meta.Parent
			if d, derr := s.reader.SessionDiff(r.Context(), session.SessionID(meta.Parent), sid); derr == nil {
				shape.Diff = d
			}
			break
		}
	}
	writeJSON(w, "loop", shape)
}

// sessionsOf is this workspace's sessions, or none when they cannot be read. A screen that is
// showing a loop map does not fail because the list beside it could not be built.
func (s *server) sessionsOf(r *http.Request, workdir string) []session.SessionMeta {
	list, err := s.reader.ListSessions(r.Context(), workdir)
	if err != nil {
		return nil
	}
	return list
}
