package main

import (
	"net/http"
	"strings"

	"github.com/sayaya1090/magi/internal/core/auth"
)

// Who may do what, at the one place it can be enforced.
//
// # A table, and a route missing from it is refused
//
// The alternative is a check inside each handler, which is a list somebody maintains: the next
// route arrives, nobody remembers, and it is open. Here the wrapper asks the table, and a path
// with no entry answers 403 with a sentence saying the table is what is missing. A test walks the
// handler list and fails on any path that is in neither set, so the decision is made when the
// route is written rather than the first time somebody is surprised by it.
//
// # Reads and writes are separated by the method, not by two entries
//
// Every GET is `read`. What a route needs is only interesting when it CHANGES something, which is
// also the split the audit record already makes — so the table has one line per route rather than
// two, and the pair cannot drift.
//
// # Hiding is not the check
//
// The page asks /me and leaves out the controls somebody may not use, because a composer for a
// person who may not prompt is a box that swallows what they typed. The server refuses regardless.
// Confusing the two is how a "read-only" console turns out to have been writable by anybody who
// opened the network tab.

// mayWrite is the capability each state-changing route needs. Read-only routes are in openToRead
// or public; anything in neither is refused, loudly.
var mayWrite = map[string]auth.Capability{
	// Giving the agent work, including moving it to another of its conversations — which is the
	// same act as prompting, one step earlier.
	"/submit":   auth.Prompt,
	"/dispatch": auth.Prompt,
	"/compact":  auth.Prompt,
	"/resume":   auth.Prompt,
	// Unblocking it, and stopping it. Apart from prompting on purpose: somebody trusted to answer
	// "yes, run that" is not necessarily somebody who decides what the companion works on, and
	// that is the most common shape of a second person on a console.
	"/answer":    auth.Answer,
	"/interrupt": auth.Answer,
	// Asking the model to look at a file somebody is editing. It changes nothing and records
	// nothing — but it spends the backend on their behalf, which is what `prompt` is about.
	"/look": auth.Prompt,
	// Changing a file in the workspace from the console. Behind `shell` rather than a capability of
	// its own: anybody who may run a command there can already write any file in it, so this widens
	// nothing — and two capabilities permitting one act would make granting either meaningless.
	// The companion records the edit in its own log; see files.go.
	"/save":    auth.Shell,
	"/git-do":  auth.Shell,
	"/file-do": auth.Shell,
	// What it learns from.
	"/skills":        auth.Curate,
	"/forget":        auth.Curate,
	"/remember":      auth.Curate,
	"/report-format": auth.Curate,
	// How it runs.
	"/model": auth.Configure,
	// Who may use this console, and how much. The capability that grants the others, and until
	// this route existed it granted nothing at all — a word in a config file that read like a
	// promise, which is the thing the auth package warns about in its own opening comment.
	"/access":     auth.Admin,
	"/permission": auth.Configure,
	"/cron":       auth.Configure,
	"/mcp":        auth.Configure,
	// A command in the workspace, outside the permission policy a tool call goes through. On a
	// console started with -exposed the route refuses before this is reached (see refuseWhenShared)
	// — this is what governs the console that is NOT shared but has more than one person on it.
	"/shell": auth.Shell,
	// Subscribing a browser to notifications. It is a write, and it is about the READER: it changes
	// where this console pushes, not anything a companion does. Read is the honest requirement —
	// anything more would stop a viewer from being told the thing they are allowed to look at.
	"/push": auth.Read,
}

// mayRead is the exception to "every GET is read": a route whose ANSWER is privileged.
//
// One route so far, and the reason is the shape of what it returns. The people list is who exists
// here, what each may do and which companions they are narrowed to — a map of the console's own
// permission model, which is the first thing somebody works from when they want more of it than
// they have. Reading it is an admin's business.
//
// A table rather than a check inside the handler, so the gate stays the one place a route's
// permissions are written down. A check in the handler is a permission nobody can find.
var mayRead = map[string]auth.Capability{
	"/access": auth.Admin,
}

// openToRead is every route that only looks. Named rather than derived, so that adding a route and
// forgetting it is a refusal instead of an opening.
var openToRead = map[string]bool{
	"/fleet": true, "/events": true, "/transcript": true, "/history": true, "/search": true,
	"/context": true, "/plan": true, "/handoffs": true, "/subagents": true, "/jobs": true,
	"/interventions": true, "/council": true, "/loop": true, "/tools": true, "/console": true,
	// The workspace, through the companion's own read-only tools. `read`, and not more: everything
	// a transcript already shows a reader includes the files the agent read and wrote, so a
	// capability that grants the transcript and refuses the tree would be drawing a line the
	// content does not respect. Writing has no route at all — see files.go.
	"/files": true, "/file": true, "/find": true, "/git": true, "/diff": true,
}

// public is the page itself and what it is made of.
//
// Not a hole: it is an empty shell that fetches everything through the routes above, and refusing
// it would hand somebody a broken browser instead of a screen that can say "ask an operator".
// The login page, when there is one, belongs here for the same reason.
var public = map[string]bool{
	"/": true, "/vendor/": true, "/i18n/": true, "/font/": true,
	"/icon.svg": true, "/icon-maskable.svg": true, "/manifest.webmanifest": true, "/sw.js": true,
	// And the answer to "who am I here", which has to reach somebody the console will refuse
	// everything else to — it is how the page knows to say so instead of drawing an empty fleet.
	// It discloses nothing they did not send: their own name back, and what it buys them.
	"/me": true,
}

// mayDo wraps a route with the question "may this person".
//
// ⚠ It is the FRONT DOOR and not the whole of a scope. Two other shapes exist and neither is
// answerable here: a route that returns a LIST has no companion in its request to check (see
// s.seen, used by /fleet and /interventions), and a route that resolves its subject from the body
// passes this check under one name and acts under another (see /dispatch, which asks again once it
// knows). Adding a route of either shape and stopping at this wrapper leaves a scope that hides
// nothing.
func (s *server) mayDo(path string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if public[path] {
			h(w, r)
			return
		}
		who := s.whoFrom(r)
		need, ok := mayWrite[path]
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			need, ok = auth.Read, ok || openToRead[path]
			if stricter, special := mayRead[path]; special {
				need, ok = stricter, true
			}
		}
		if !ok {
			// The table, not the person. Said as a refusal because the alternative — assuming a
			// route nobody classified is harmless — is how a permission model quietly stops
			// covering the thing that was added last.
			http.Error(w, "this console does not know what permission "+path+" needs, so it "+
				"refuses it", http.StatusForbidden)
			return
		}
		// A console with nobody configured is the one-operator console and answers everything —
		// the short-circuit lives here rather than inside the grant, because a grant is about a
		// caller and this is about whether there is a policy at all.
		if s.policy.Configured() &&
			!s.grant(r).Allows(need, nameOfSocket(r.URL.Query().Get("d")), r.URL.Query().Get("p")) {
			http.Error(w, refusal(who, need), http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// seen reports whether a companion belongs on this person's screen, for the routes that answer
// with a LIST. The gate cannot do this: there is no companion in the request to check, and the
// answer is what has to be filtered.
func (s *server) seen(r *http.Request, name, peer string) bool {
	if !s.policy.Configured() {
		return true
	}
	return s.grant(r).InScope(name, peer)
}

// onlySeen keeps the rows of a list whose subject this person may see.
//
// The third shape of a scope check, and the one that is easy to forget because nothing about the
// request is wrong: the caller asked a legitimate question about no companion in particular, and
// the ANSWER is what carries other people's companions. A gate reading the query string has
// nothing to inspect here — the subject appears only once the list is built — so the filter has to
// live where the list is. One helper rather than a loop written out at each site, because a scope
// implemented four times is four slightly different scopes.
// It copies rather than filtering in place. The rows here are tens, not thousands, and a shared
// helper that quietly empties its caller's slice is a trap for the first route that hands it
// something cached — the cache would keep one person's scope and serve it to everybody.
func onlySeen[T any](s *server, r *http.Request, rows []T, of func(T) (name, peer string)) []T {
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		if name, peer := of(row); s.seen(r, name, peer) {
			out = append(out, row)
		}
	}
	return out
}

// refusal says which of the two things is missing, because they need different next steps: an
// unnamed caller has to fix the gateway, and a named one has to be given a role by somebody.
func refusal(who string, need auth.Capability) string {
	if who == "" {
		return "this console is shared and nothing in front of it said who you are (see " +
			"-user-header), so it cannot check whether you may " + string(need)
	}
	return who + " may not " + string(need) + " here — ask an operator"
}

// nameOfSocket is the companion a request is about, by name, for the per-person scope.
//
// The socket path is what the page sends and what everything else here resolves; the NAME is what
// a person writes in auth.toml, because a path is a fact about this machine's filesystem and not
// something anybody wants to type into a permission file.
func nameOfSocket(socket string) string {
	if socket == "" {
		return ""
	}
	base := socket
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimPrefix(base, "daemon-")
	base = strings.TrimSuffix(base, ".sock")
	// The same trailing hash the page strips: two workspaces of the same name get one each, and a
	// permission written against the name must match both or neither.
	if i := strings.LastIndexByte(base, '-'); i > 0 && len(base)-i == 9 {
		base = base[:i]
	}
	return base
}

// me is what the page needs to draw itself: who this is, and what they may do.
//
// It exists so the console can leave out what would only produce a refusal. It is not the check —
// see the note at the top of this file — and it deliberately answers for an unconfigured console
// too, where the answer is "everything", so the page has one shape of answer to read.
func (s *server) me(w http.ResponseWriter, r *http.Request) {
	who := s.whoFrom(r)
	can := s.policy.CanWith(who, s.groupsFrom(r))
	out := struct {
		Who        string            `json:"who,omitempty"`
		Can        []auth.Capability `json:"can"`
		Companions []string          `json:"companions,omitempty"`
		Shared     bool              `json:"shared,omitempty"`
	}{Who: who, Can: can, Companions: s.policy.ScopeWith(who, s.groupsFrom(r)), Shared: s.shared()}
	if out.Can == nil {
		out.Can = []auth.Capability{}
	}
	writeJSON(w, "capabilities", out)
}
