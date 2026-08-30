package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/app"
)

// Finding a session by something said in it.
//
// The history card lists what a companion has done, newest first, and that answers "what has it
// been doing" and not "where was the thing about the vacuum threshold". A title is the first thing
// that was asked; a week later the thing being looked for was said in the middle.
//
// It searches TURNS and returns sessions, which is the same call the terminal's picker makes —
// two rankings over one corpus would agree until one of them was fixed.

// searchHit is one session a search matched, as the page draws it.
type searchHit struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	When  string `json:"when"`
	// Turns is how many turns matched, which can be more than the snippets shown.
	Turns    int          `json:"turns"`
	Snippets []searchSnip `json:"snippets"`
	// Scheduled names the job that opened this session, when one did. Unattended work reads very
	// differently from something a person asked for.
	Scheduled string `json:"scheduled,omitempty"`
}

type searchSnip struct {
	Ref    string `json:"ref"`
	Prompt string `json:"prompt"`
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, "search", []searchHit{})
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Workdir == "" {
		writeJSON(w, "search", []searchHit{})
		return
	}
	hits, err := s.reader.RankSessions(r.Context(), in.Workdir, q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]searchHit, 0, len(hits))
	for _, h := range hits {
		row := searchHit{ID: string(h.Meta.ID), Title: h.Meta.Title, Turns: h.Turns,
			When: h.Meta.Created.Format(time.RFC3339)}
		if name, ok := app.CronOriginName(h.Meta.Origin); ok {
			row.Scheduled = name
		}
		for _, t := range h.Snippets {
			row.Snippets = append(row.Snippets, searchSnip{Ref: t.Ref(h.Meta.ID), Prompt: t.Prompt})
		}
		out = append(out, row)
	}
	writeJSON(w, "search", out)
}
