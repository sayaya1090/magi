package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
)

// The wiki half of the knowledge screen: every canonical page this console governs, with the
// facts a reader needs to trust or retire one — who edited it last, when, in which tier, and the
// stale tombstones the agent-facing index deliberately hides. Same tier enumeration as /skills:
// the machine's global store, every team with a footprint here, and every published companion's
// project tier.
type storedPage struct {
	Title   string   `json:"title"`
	Hook    string   `json:"hook"` // first non-blank body line — the list's one-liner
	Body    string   `json:"body"`
	Links   []string `json:"links,omitempty"`
	Stale   bool     `json:"stale,omitempty"`
	Updated string   `json:"updated,omitempty"`
	Editor  string   `json:"editor,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Tier    string   `json:"tier"`
	Team    string   `json:"team,omitempty"`
	Comp    string   `json:"companion,omitempty"`
}

func (s *server) wiki(w http.ResponseWriter, r *http.Request) {
	out := []storedPage{}
	add := func(dir, tier, team, comp string) {
		for _, p := range expgit.New(dir).WikiList(r.Context()) {
			out = append(out, storedPage{
				Title: p.Title, Hook: hookLine(p.Body), Body: p.Body, Links: p.Links,
				Stale: p.Stale, Updated: p.Updated, Editor: p.Editor, Summary: p.Summary,
				Tier: tier, Team: team, Comp: comp,
			})
		}
	}
	add(filepath.Join(s.cfgDir, "experience"), "global", "", "")
	if teams, err := os.ReadDir(filepath.Join(s.cfgDir, "teams")); err == nil {
		for _, t := range teams {
			if t.IsDir() {
				add(filepath.Join(s.cfgDir, "teams", t.Name(), "experience"), "team", t.Name(), "")
			}
		}
	}
	if local, err := s.companionDirs(r); err == nil {
		for _, c := range local {
			add(filepath.Join(c.workdir, ".magi", "experience"), "project", "", c.name)
		}
	}
	writeJSON(w, "wiki pages", out)
}

func hookLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
