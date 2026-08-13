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

type accessAnswer struct {
	// Instance is whose list this is: user@host, and the config directory it was read from.
	//
	// It answers a question the screen used to leave open. A fleet spans machines and accounts, and
	// this list governs exactly ONE of them — the one this console runs as — so a roster with no
	// name on it reads as the roster of everything on the page. Two accounts on one machine have
	// two of these and neither can see the other's; the directory is beside the name because
	// MAGI_CONFIG_DIR can give one account two instances, and then it is the only thing that tells
	// them apart.
	//
	// It comes from the same read as the rows rather than from /console, so the name and the list
	// cannot come from two different processes and disagree.
	Instance instanceRow `json:"instance"`
	People   []personRow `json:"people"`
	// Groups is the other half of the roster, and on a console wired to a directory it is the
	// whole of it: membership is maintained where somebody is hired and let go, and what is listed
	// here is the mapping onto what that means. A screen that showed only individuals would say
	// "nobody has access" about a console the whole team uses.
	Groups []personRow `json:"groups,omitempty"`
	// Roles is what a role name may be here, so a screen offers the ones this console has rather
	// than a free-text box that fails on save.
	Roles []roleRow `json:"roles"`
	// Configured is false on a console with nobody listed: one operator, no policy, and adding the
	// first person is the act that turns the gate on for everybody.
	Configured bool `json:"configured"`
	// Named is whether anything in front of this console says who a request is from — a gateway
	// setting the header named by -user-header.
	//
	// Without it the FIRST person cannot be added: naming somebody turns the gate on, and a console
	// that cannot tell who anybody is would then refuse everybody, including the person who just
	// did it. accessWrite refuses that with a sentence, which is right, but the screen was still
	// offering the button — a name typed into a dialog, the dialog closing, and nothing written.
	// The screen can only decline to offer it if it is told, so it is told.
	Named bool `json:"named"`
}

type instanceRow struct {
	Who       string `json:"who,omitempty"`       // user@host
	ConfigDir string `json:"configDir,omitempty"` // where the file it governs lives
}

type roleRow struct {
	Name string            `json:"name"`
	Can  []auth.Capability `json:"can"`
}

// people answers with the list, or changes one entry.
func (s *server) access(w http.ResponseWriter, r *http.Request) {
	// Never forwarded to a peer. A policy belongs to the console that enforces it, and editing
	// another machine's from here would be an admin on one console quietly becoming an admin on
	// another.
	if r.Method == http.MethodPost {
		s.accessWrite(w, r)
		return
	}
	p, err := config.LoadAuth(s.cfgDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	me := strings.ToLower(strings.TrimSpace(s.whoFrom(r)))
	user, host := whereWeAre()
	out := accessAnswer{
		Instance:   instanceRow{Who: instanceOf(user, host), ConfigDir: s.cfgDir},
		Configured: p.Configured(),
		Named:      s.userHeader != "",
	}
	for who, person := range p.People {
		out.People = append(out.People, personRow{
			Who: who, Role: person.Role, Companions: person.Companions,
			Can: p.Can(who), Me: who == me,
		})
	}
	sort.Slice(out.People, func(i, j int) bool { return out.People[i].Who < out.People[j].Who })
	for name, person := range p.Groups {
		out.Groups = append(out.Groups, personRow{
			Who: name, Role: person.Role, Companions: person.Companions,
			Can: p.Roles[person.Role].Can,
		})
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Who < out.Groups[j].Who })
	for name, role := range p.Roles {
		out.Roles = append(out.Roles, roleRow{Name: name, Can: role.Can})
	}
	sort.Slice(out.Roles, func(i, j int) bool { return out.Roles[i].Name < out.Roles[j].Name })
	writeJSON(w, "people", out)
}

func (s *server) accessWrite(w http.ResponseWriter, r *http.Request) {
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

// claimHint says, once, how to become the admin of a console nobody has claimed.
//
// The console cannot work out that the person behind the gateway's header is the one who started
// it: the daemon recorded a uid and the header carries a name, and nothing joins them. So the
// answer is to say the name out loud where the person who CAN join them will see it — a terminal
// on that machine — with the command that does it.
//
// Once per process, because it is a note and not an alarm, and never on a console that already has
// a policy: there is nothing to claim then, and the sentence would read as an offer.
func (s *server) claimHint(r *http.Request) {
	if s.policy.Configured() || s.userHeader == "" {
		return
	}
	who := s.whoFrom(r)
	if who == "" {
		return
	}
	s.claimOnce.Do(func() {
		log.Printf("magi-web: nobody is configured, so everybody who reaches this console is the "+
			"operator. The gateway calls you %s — claim it on that machine with: "+
			"magi --grant %s --role operator", who, who)
	})
}

// claiming wraps a route so the hint above gets a chance to be said.
//
// Around the gate rather than inside a handler: the note is about the console and not about any
// one route, and on an unclaimed console every route is answering everybody anyway.
func (s *server) claiming(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.claimHint(r)
		h(w, r)
	}
}
