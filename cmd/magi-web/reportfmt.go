package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/core/report"
)

// The shape of the report a companion has to fill in before it may ask somebody a question.
//
// # Why this is a screen at all
//
// The sections are a CONTRACT — ask_user refuses a report with one missing — and until now the only
// way to change them was to write a markdown file into a workspace over ssh. The person the report
// is FOR is the one who knows what they need in it, and they are the person looking at this console
// at 9pm deciding something an agent worked on all afternoon. A contract nobody can edit from where
// they read it is a contract that stays at whatever three sections the default happened to pick.
//
// # Where it is read from and where it is written
//
// Read: this companion's workspace, then the console's own directory, then the built-in default —
// the same order the agent's own lookup walks, so what this screen shows is what the agent will be
// held to. Written: the WORKSPACE, always. A console can hold companions belonging to several
// projects and "change the report format" pressed on one of them must not silently re-shape every
// other one; the global file stays a thing somebody edits deliberately, by hand.

// reportFormat is what the page reads: the effective sections and where they came from.
type reportFormat struct {
	// From is "workspace", "console" or "default" — the tier this contract came out of, because
	// "edit" means something different in each: the first is yours to change here, the second is
	// shared with every companion under this console, and the third does not exist yet.
	From     string          `json:"from"`
	Sections []reportSection `json:"sections"`
}

type reportSection struct {
	Key    string `json:"key"`
	Prompt string `json:"prompt"`
}

// skillFile is where a named skill lives in a tier. Flat .md, which is the layout the agent's own
// loader reads — a directory-shaped skill is invisible to it.
func skillFile(dir string) string {
	return filepath.Join(dir, "skills", report.SkillName+".md")
}

func (s *server) reportFormat(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if r.Method == http.MethodPost {
		s.writeReportFormat(w, r, in.Workdir)
		return
	}
	out := reportFormat{From: "default"}
	body := ""
	if b, rerr := os.ReadFile(skillFile(filepath.Join(in.Workdir, ".magi"))); rerr == nil {
		body, out.From = string(b), "workspace"
	} else if b, rerr := os.ReadFile(skillFile(s.cfgDir)); rerr == nil {
		body, out.From = string(b), "console"
	}
	c := report.Default
	if body != "" {
		if parsed := report.Parse(body); len(parsed) > 0 {
			c = parsed
		}
	}
	for _, sec := range c {
		out.Sections = append(out.Sections, reportSection{Key: sec.Key, Prompt: sec.Prompt})
	}
	writeJSON(w, "report format", out)
}

// writeReportFormat rewrites this companion's copy of the skill from the sections it was given.
//
// The whole file, not a patch. A skill is prose with a section block in it and the block is the
// only part this screen understands; rewriting around the prose would mean parsing a document to
// preserve the half nobody edited, and getting that wrong loses somebody's writing. So the file
// this produces says plainly that it was written here, and anybody who wants prose in it edits the
// file — at which point this screen still reads their sections back correctly.
func (s *server) writeReportFormat(w http.ResponseWriter, r *http.Request, workdir string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "unreadable form", http.StatusBadRequest)
		return
	}
	keys := r.PostForm["key"]
	prompts := r.PostForm["prompt"]
	var c report.Contract
	seen := map[string]bool{}
	for i, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		// One key twice is a report with one of them unreachable: Fill writes by key, so the
		// second would overwrite the first and the person would be missing a section they wrote.
		if seen[k] {
			http.Error(w, "two sections named "+k, http.StatusBadRequest)
			return
		}
		seen[k] = true
		p := ""
		if i < len(prompts) {
			p = strings.TrimSpace(prompts[i])
		}
		c = append(c, report.Section{Key: k, Prompt: p})
	}
	if len(c) == 0 {
		// Not an empty contract: ask_user would then accept a report with nothing in it, which is
		// the state this whole mechanism exists to prevent. Removing the file is how somebody goes
		// back to the default, and that is a different button.
		http.Error(w, "a report needs at least one section", http.StatusBadRequest)
		return
	}
	var b strings.Builder
	b.WriteString("Ask with a report: fill every section below before putting a decision to somebody.\n\n")
	b.WriteString("Written from the console. Prose you add outside the section block is kept by the\n")
	b.WriteString("agent's own reader and ignored here.\n\n## sections\n")
	for _, sec := range c {
		b.WriteString("- " + sec.Key)
		if sec.Prompt != "" {
			b.WriteString(": " + sec.Prompt)
		}
		b.WriteString("\n")
	}
	path := skillFile(filepath.Join(workdir, ".magi"))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Atomic, because the agent reads this file whenever it is about to ask something: a
	// truncate-then-write between those two is a contract read as empty, which is a report with no
	// sections at the moment somebody needed one.
	if err := atomicfile.Write(path, []byte(b.String()), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
