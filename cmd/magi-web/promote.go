package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/port"
)

// Turning something a person had to say into something they will not have to say again.
//
// This is the point of the whole supervision loop: one person holds several companions only if each
// intervention permanently removes future ones. A correction that stays in a transcript is a
// correction you will give again next week.
//
// # Where it goes, and why the person picks
//
// Two tiers, and the difference is the companion boundary:
//
//	project — <workspace>/.magi/experience, and it stays with that companion (and its repo, so the
//	          team gets it too). "The auth service uses X" belongs here.
//	global  — <config>/experience, shared by every companion this person runs. "Run the tests
//	          before you say it is done" belongs here.
//
// magi does not choose. Promoting a project fact to global leaks one project's truth into another's
// prompts, and it leaks quietly — nobody finds the cause weeks later. The person knows which kind
// they just said; the console asks.
//
// # Why the console writes it directly
//
// The alternative was to tell the companion to remember it, which spends a model turn on a decision
// a human has already made — and puts the wording through a paraphrase, which is how the identifier
// in a rule gets lost. Direct writing is safe here because the store's writes are atomic now and
// the global tier already has several writers: every companion of this person writes to it.
type promotion struct {
	Text      string // what the person said, verbatim
	Scope     string // "project" | "global"
	Companion string // the socket of the companion whose project tier to write to
	Peer      string // set when that companion is on another console
}

// promoteRoute is the handler.
func (s *server) promote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	scope := r.FormValue("scope")
	if text == "" {
		http.Error(w, "nothing to promote", http.StatusBadRequest)
		return
	}
	if scope != "project" && scope != "global" {
		// Named, not defaulted. The two tiers differ in whether the lesson crosses the companion
		// boundary, and a default would make that crossing happen by omission.
		http.Error(w, "scope must be project or global", http.StatusBadRequest)
		return
	}

	// A companion on another console is promoted THERE: its workspace is that machine's path and
	// its store is that machine's directory.
	if p, _, remote, err := s.routeToPeer(r); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if remote {
		s.proxy(w, r, p, r.URL.Query().Get("d"))
		return
	}

	dir, err := s.storeDirFor(r, scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := writeRule(r.Context(), dir, text, scope); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// storeDirFor resolves which experience directory a promotion lands in.
//
// The project tier's path comes from the PUBLISHED companion, never from the request. A page that
// could name the directory could name any directory, and this process writes files there.
func (s *server) storeDirFor(r *http.Request, scope string) (string, error) {
	if scope == "global" {
		return filepath.Join(s.cfgDir, "experience"), nil
	}
	in, err := s.target(r)
	if err != nil {
		return "", fmt.Errorf("a project rule needs the companion it belongs to: %w", err)
	}
	if in.Workdir == "" {
		return "", fmt.Errorf("that companion published no workspace, so there is nowhere to put a project rule")
	}
	return filepath.Join(in.Workdir, ".magi", "experience"), nil
}

// writeRule records the correction as a skill in one tier.
//
// A skill rather than a memory: a memory is something that happened, and this is something to do —
// which is what the retrieval puts in front of the model as guidance. The body carries the words
// verbatim and says where they came from, because a rule whose origin is lost cannot be argued with
// later, and the first question about any rule is who decided it.
func writeRule(ctx context.Context, dir, text, scope string) error {
	store := expgit.New(dir)
	return store.Propose(ctx, port.Contribution{
		Scope: scope,
		Skills: []port.Skill{{
			Name:        ruleName(text),
			Description: firstLine(text),
			Body: text + "\n\n— a standing instruction, promoted by the person supervising this " +
				"companion on " + time.Now().Format("2006-01-02") + " from something they had to say " +
				"mid-turn.",
		}},
	})
}

// ruleName is a file name for the rule: the first few words, which is what a person scanning the
// skills directory would look for.
func ruleName(text string) string {
	words := strings.Fields(strings.ToLower(firstLine(text)))
	if len(words) > 6 {
		words = words[:6]
	}
	var keep []rune
	for _, r := range strings.Join(words, "-") {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			keep = append(keep, r)
		case r > 127:
			keep = append(keep, r) // a rule written in Korean keeps its own letters
		}
	}
	name := strings.Trim(string(keep), "-")
	if name == "" {
		return "rule"
	}
	return name
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
