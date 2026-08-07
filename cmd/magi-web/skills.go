package main

import (
	"context"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
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

	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier == "global" // the crossing tier first: it is the one with reach
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
