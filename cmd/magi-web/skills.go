package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/core/text"
	"github.com/sayaya1090/magi/internal/port"
)

// What this person's companions have learned, and which of it crosses between them.
//
// The store is already two tiers and the boundary between them is the whole of context hygiene:
// project knowledge stays with a companion and its repo, craft carries across. That boundary is
// only as good as somebody's ability to SEE it — a rule that quietly sits in the global tier
// reaches every prompt on every project, and nothing else in the system would ever mention it
// again after the day it was written.
//
// So this is a governance screen and not a browser: it lists every tier of every companion, says
// which tier each entry is in, and lets a wrong one be removed. Growth-only stores stop being
// promoted into, because the cost of a mistake is permanent.
type storedSkill struct {
	expgit.SkillInfo
	Tier      string `json:"tier"`                // "project" | "global"
	Companion string `json:"companion,omitempty"` // whose project tier, when it is one
	Socket    string `json:"socket,omitempty"`    // how a delete names it back
	Team      string `json:"team,omitempty"`      // whose team tier, when it is one
	Peer      string `json:"peer,omitempty"`      // the console it lives on, when it is not this one
}

func (s *server) skills(w http.ResponseWriter, r *http.Request) {
	out := []storedSkill{}

	// The global tier: one per console, shared by every companion under it.
	global := expgit.New(filepath.Join(s.cfgDir, "experience"))
	if list, err := global.Inventory(r.Context()); err == nil {
		for _, sk := range list {
			out = append(out, storedSkill{SkillInfo: sk, Tier: "global"})
		}
	}

	// The team tiers: one directory per team that a companion on this machine has declared. Read
	// from the directory rather than from the fleet, because a team's knowledge outlives the
	// companions that wrote it — disband the team today and what it decided is still governed here
	// tomorrow, which is exactly the case a page that listed only live teams would hide.
	teams, terr := os.ReadDir(filepath.Join(s.cfgDir, "teams"))
	if terr == nil {
		for _, t := range teams {
			if !t.IsDir() {
				continue
			}
			st := expgit.New(filepath.Join(s.cfgDir, "teams", t.Name(), "experience"))
			list, ierr := st.Inventory(r.Context())
			if ierr != nil {
				continue
			}
			for _, sk := range list {
				out = append(out, storedSkill{SkillInfo: sk, Tier: "team", Team: t.Name()})
			}
		}
	}

	// And every companion's own, from the published list — the same allowlist every other path
	// here uses, so a directory nobody published is not a directory this process reads.
	local, err := s.companionDirs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, c := range local {
		st := expgit.New(filepath.Join(c.workdir, ".magi", "experience"))
		list, ierr := st.Inventory(r.Context())
		if ierr != nil {
			continue
		}
		for _, sk := range list {
			out = append(out, storedSkill{SkillInfo: sk, Tier: "project", Companion: c.name, Socket: c.socket})
		}
	}
	// And every federated console's, the same way the fleet merges: a supervisor watching three
	// machines is governing three global tiers, and a page that showed only this one would say
	// "three rules" about a store that holds nine.
	out = append(out, s.peerSkills(r.Context())...)

	// A rule belongs to the workspace it governs, so a project tier is that companion's and is
	// filtered like anything else of theirs. The global and team tiers name no companion and are
	// kept: they are this console's own, and a person who may read here may read what governs
	// everybody. See onlySeen for why this cannot be done at the door.
	out = onlySeen(s, r, out, func(k storedSkill) (string, string) { return k.Companion, k.Peer })

	// Widest reach first: what every companion follows, then what a team follows, then one
	// workspace. A page about who a rule reaches is ordered by how far it reaches.
	rank := map[string]int{"global": 0, "team": 1, "project": 2}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return rank[out[i].Tier] < rank[out[j].Tier]
		}
		if out[i].Team != out[j].Team {
			return out[i].Team < out[j].Team
		}
		if out[i].Peer != out[j].Peer {
			return out[i].Peer < out[j].Peer // this console's own first: the empty name sorts before any
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, "skills", out)
}

// forgetSkill removes one stored skill. (Named apart from forget, which drops a cached socket
// connection — one word for "stop remembering this lesson" and "reconnect to that daemon" would be
// a method name that means two things.)
//
// A promoted rule that turned out to be wrong has to be removable, or the store only grows and a
// supervisor stops promoting into it — the cost of a mistake being permanent is what makes people
// stop using a thing.
func (s *server) forgetSkill(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	tier := r.FormValue("tier")
	if name == "" {
		http.Error(w, "no skill named", http.StatusBadRequest)
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	dir, err := s.storeDirFor(r, tier)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := expgit.New(dir).Forget(r.Context(), name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Writing something down on purpose, from the console.
//
// # Why a person needs this door at all
//
// Everything in the store arrived one way: an agent decided, mid-turn, that something was worth
// keeping and called `remember`. That is the right way for most of it — the agent is the one who
// just learned the thing — but it left no way at all for a PERSON to say "this is a rule now". A
// supervisor who has told three companions the same thing has to wait for one of them to write it
// down, and hope it lands in the tier they meant.
//
// # Why it says who wrote it
//
// The source line separates the two. An agent's entry carries what it was doing when it learned it
// (internal/app/execute.go); this carries the person and the console. Reading a rule you did not
// write, the first question is where it came from, and "somebody decided this" and "an agent
// inferred it from one afternoon" deserve different amounts of trust.
//
// # Why it writes the file rather than asking a daemon
//
// The same reason /forget does: the store is a directory of files and the console already reads and
// deletes from it. Routing a write through a daemon would mean picking WHICH daemon, and a team's
// store belongs to no single companion — including a team that has nobody running at all.
func (s *server) remember(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	body := strings.TrimSpace(r.FormValue("text"))
	if body == "" {
		http.Error(w, "nothing to write down", http.StatusBadRequest)
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	dir, err := s.storeDirFor(r, r.FormValue("tier"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// A fact, not a skill. A skill is a named procedure with a body, and a person typing one line
	// into a box is stating something true — inventing a name for it would put a heading on the
	// screen that nobody wrote.
	c := port.Contribution{
		Memories: []port.Memory{{Text: body}},
		Source:   "console",
	}
	if who := strings.TrimSpace(r.FormValue("who")); who != "" {
		c.Source = "console · " + text.Clip(who, 60)
	}
	if err := expgit.New(dir).Propose(r.Context(), c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// peerSkills asks every federated console what it holds.
//
// Same failure rule as the fleet merge: a console that does not answer costs its own rows and
// nothing else. No placeholder row here — the fleet view is already showing that machine as
// unreachable, and a second "could not reach" on this page would say nothing the first did not.
func (s *server) peerSkills(ctx context.Context) []storedSkill {
	var out []storedSkill
	for _, r := range fanOut(ctx, s.peers, func(ctx context.Context, p peer) ([]storedSkill, error) {
		got, err := getJSON[storedSkill](ctx, s.http, p.Base+"/skills")
		for j := range got {
			// Stamped with the console that answered, and never with what IT called a peer: a
			// console two hops away is not one this operator configured, and an action routed to
			// that name would find nothing here.
			got[j].Peer = p.Name
		}
		return got, err
	}) {
		out = append(out, r.List...) // an error leaves an empty list, which is the whole handling
	}
	return out
}

// companion is the little of a published record this file needs.
type companion struct{ name, workdir, socket string }

// companionDirs is the published companions of THIS console, which is the only set whose
// directories this process will read.
func (s *server) companionDirs(r *http.Request) ([]companion, error) {
	// Published, not probed: this is a directory listing and does not need to know who is alive —
	// a companion's rules are on disk whether or not its daemon is running, and dialling five
	// sockets to render a list of files is the cost the fleet view pays for a reason this one does
	// not have.
	list, err := daemon.List(s.cfgDir)
	if err != nil {
		return nil, err
	}
	out := make([]companion, 0, len(list))
	for _, in := range list {
		if in.Workdir == "" {
			continue
		}
		out = append(out, companion{name: filepath.Base(in.Workdir), workdir: in.Workdir, socket: in.Socket})
	}
	return out, nil
}

// storeDirFor is the experience directory a tier lives in: this console's own for a global rule,
// the companion's workspace for a project one.
//
// It lived next to the promotion endpoint until that was removed — the pipeline it served needed
// somebody to visit a screen and curate, which nobody does — and forgetting still needs to know
// where a rule is kept.
func (s *server) storeDirFor(r *http.Request, scope string) (string, error) {
	if scope == "global" {
		return filepath.Join(s.cfgDir, "experience"), nil
	}
	// A team tier is named, not resolved from a companion: the team outlives its members, and
	// asking "which companion is this for" would fail for a team with nobody running.
	if team := strings.TrimSpace(r.FormValue("team")); scope == "team" && team != "" {
		if strings.ContainsAny(team, `/\.`) {
			return "", fmt.Errorf("a team name is one path segment, and %q is not", team)
		}
		return filepath.Join(s.cfgDir, "teams", team, "experience"), nil
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
