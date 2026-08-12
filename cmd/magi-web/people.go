package main

import (
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/auth"
)

// Who may use this console, and how much.
//
// # Why this exists at all
//
// The policy has been a file somebody edits by hand, which is fine for writing it once and wrong
// for everything after: the person who needs to add a colleague on a Tuesday is not always the
// person with a shell on that machine, and `admin` — the capability that is supposed to grant the
// others — granted nothing, because no route asked for it. A capability with no route behind it is
// exactly what the auth package's opening comment says not to have.
//
// # The file stays the source of truth
//
// This edits auth.toml surgically and re-reads it; it does not keep a copy. Two ideas of who may
// do what is the failure this avoids, and the file is also what a machine with no console still
// has.
//
// # The change that must not be possible
//
// Locking the door from the inside. A console whose file lists people and gives none of them
// `admin` refuses to start, so demoting or deleting the last one is a change whose consequence is
// discovered as a console that will not come back — with the fix behind the door. config.SetPerson
// checks the RESULT rather than the edit, because three different edits have that one consequence.
type personRow struct {
	Who        string            `json:"who"`
	Role       string            `json:"role"`
	Companions []string          `json:"companions,omitempty"`
	Can        []auth.Capability `json:"can"`
	// Me marks the row of whoever is asking, so a screen can warn before somebody edits themselves
	// out of the ability to edit.
	Me bool `json:"me,omitempty"`
}

type peopleAnswer struct {
	People []personRow `json:"people"`
	// Roles is what a role name may be here, so a screen offers the ones this console has rather
	// than a free-text box that fails on save.
	Roles []roleRow `json:"roles"`
	// Configured is false on a console with nobody listed: one operator, no policy, and adding the
	// first person is the act that turns the gate on for everybody.
	Configured bool `json:"configured"`
}

type roleRow struct {
	Name string            `json:"name"`
	Can  []auth.Capability `json:"can"`
}

// people answers with the list, or changes one entry.
func (s *server) people(w http.ResponseWriter, r *http.Request) {
	// Never forwarded to a peer. A policy belongs to the console that enforces it, and editing
	// another machine's from here would be an admin on one console quietly becoming an admin on
	// another.
	if r.Method == http.MethodPost {
		s.peopleWrite(w, r)
		return
	}
	p, err := config.LoadAuth(s.cfgDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	me := strings.ToLower(strings.TrimSpace(s.whoFrom(r)))
	out := peopleAnswer{Configured: p.Configured()}
	for who, person := range p.People {
		out.People = append(out.People, personRow{
			Who: who, Role: person.Role, Companions: person.Companions,
			Can: p.Can(who), Me: who == me,
		})
	}
	sort.Slice(out.People, func(i, j int) bool { return out.People[i].Who < out.People[j].Who })
	for name, role := range p.Roles {
		out.Roles = append(out.Roles, roleRow{Name: name, Can: role.Can})
	}
	sort.Slice(out.Roles, func(i, j int) bool { return out.Roles[i].Name < out.Roles[j].Name })
	writeJSON(w, "people", out)
}

func (s *server) peopleWrite(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	who := strings.TrimSpace(r.FormValue("who"))
	if who == "" {
		http.Error(w, "which person", http.StatusBadRequest)
		return
	}
	if r.FormValue("remove") != "" {
		if err := config.RemovePerson(s.cfgDir, who); err != nil {
			// The refusals from there are the interesting half — "that would leave nobody who may
			// admin" is the one worth reading — so they are passed through rather than replaced
			// with a status line.
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.repolicy()
		writeText(w, who+" can no longer use this console")
		return
	}
	// The first person is the act that turns the gate on for everybody, and on a console with
	// nothing in front of it there is then nobody it can name — every request, including this
	// screen, becomes an unnamed one and is refused. The startup check catches that on the next
	// run and refuses to boot; here it would happen live, to the person who just did it.
	if !s.policy.Configured() && s.userHeader == "" {
		http.Error(w, "this console has nothing in front of it to say who anybody is, so listing "+
			"people would lock everybody out of it — start it with -user-header (and a gateway "+
			"that sets it) before giving anybody a role", http.StatusConflict)
		return
	}
	person := auth.Person{Role: strings.TrimSpace(r.FormValue("role"))}
	for _, c := range strings.Split(r.FormValue("companions"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			person.Companions = append(person.Companions, c)
		}
	}
	if err := config.SetPerson(s.cfgDir, who, person); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.repolicy()
	writeText(w, who+" is a "+person.Role+" here")
}

// repolicy re-reads the file this process is enforcing.
//
// Without it a change would take a restart to mean anything, and the person who made it would be
// looking at a console still applying the old one — which is the worst way for a permission change
// to behave, because it looks like it worked.
//
// A load that fails leaves the old policy in place and says so on the way out: the file has just
// been written by this process and re-read by it, so a failure here is a bug rather than a state
// to degrade into, and degrading would mean dropping to "nobody configured" — every request
// allowed.
func (s *server) repolicy() {
	p, err := config.LoadAuth(s.cfgDir)
	if err != nil {
		log.Printf("magi-web: the policy was changed and could not be re-read (%v); this process "+
			"is still applying the one it started with", err)
		return
	}
	s.policy = p
}
