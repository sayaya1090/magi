package main

import (
	"net/http"
	"sort"
	"time"

	"github.com/sayaya1090/magi/internal/core/text"
)

// What a companion has done before now.
//
// # Why this is missing until you look for it
//
// Every other screen here answers a question about the present: what is running, what is blocked,
// what has been learned. A companion's own page shows the turn it is in and the transcript of that
// turn, and when the turn ends the page shows the next one. So the thing a supervisor asks after
// being away — "what has this one actually been doing?" — had no answer anywhere, while the answer
// sat on disk the whole time: every session it has ever run is in the log store, with the request
// that opened it.
//
// # Derived, like everything else here
//
// Nothing new is recorded. The store already keeps one session per piece of work with the title it
// was opened under; this reads that list back and clips it. A companion that ran for a month before
// this endpoint existed has a month of history in it.
//
// # Why the title and not a summary
//
// The title is the request as it was made. A summary would be this process deciding what the work
// was about, which is a judgement it has no grounds for and one that would quietly rewrite what
// somebody asked for. The request is the one description that is not an interpretation.
type pastSession struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Started and Ended bound the work. Both, because "ran for four hours" and "ran at 2am" are
	// different facts and a supervisor going back over a week wants each of them.
	Started string `json:"started"`
	Ended   string `json:"ended"`
	// Ago is seconds since it last did anything, so the page says "3d ago" the same way the fleet
	// does rather than inventing a second way to write a time.
	Ago int `json:"ago"`
	// Model is what it was opened with. A companion's model can be changed mid-life, so the one it
	// is on now says nothing about what ran last Tuesday — and "which engine did this" is the fact
	// about a finished piece of work that is neither in its title nor recoverable afterwards.
	Model string `json:"model,omitempty"`
	// Labels is what the agent said this work was about. The one thing on a card that no derivation
	// could produce, and the reason the board can be searched by subject rather than by wording.
	Labels []string `json:"labels,omitempty"`
	// Current marks the session the companion is in right now, which is on this list too: it is
	// work it is doing, and leaving it off would make the newest row the second-newest thing.
	Current bool `json:"current,omitempty"`
}

func (s *server) history(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Workdir == "" {
		// A companion that published no workspace has no directory to look sessions up by. Say so
		// rather than answering with every session on the machine.
		writeJSON(w, "history", []pastSession{})
		return
	}
	metas, err := s.reader.ListSessions(r.Context(), in.Workdir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now()
	out := make([]pastSession, 0, len(metas))
	for _, m := range metas {
		// Child sessions are not work somebody asked for; they are how a piece of work was carried
		// out. Listing them here would put the same task on the page several times.
		if m.Parent != "" {
			continue
		}
		out = append(out, pastSession{
			ID: string(m.ID), Title: text.Clip(m.Title, 160),
			Started: m.Created.UTC().Format(time.RFC3339),
			Ended:   m.LastActivity.UTC().Format(time.RFC3339),
			Ago:     int(now.Sub(m.LastActivity).Seconds()),
			Model:   m.Model,
			Labels:  m.Labels,
			Current: string(m.ID) == in.Session,
		})
	}
	// Newest first: the reason to open this is usually the last thing, not the first.
	sort.Slice(out, func(i, j int) bool { return out[i].Ended > out[j].Ended })
	if len(out) > 50 {
		// A companion months old has hundreds. Fifty is a page somebody reads; the rest would be a
		// scroll nobody reaches the end of, and the store is still there for anyone who needs more.
		out = out[:50]
	}
	writeJSON(w, "history", out)
}
