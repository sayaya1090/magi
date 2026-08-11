package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/auth"
)

// withPolicy is a console whose auth.toml says what this test needs it to say.
func withPolicy(t *testing.T, toml string) *server {
	t.Helper()
	dir := t.TempDir()
	if toml != "" {
		if err := os.WriteFile(filepath.Join(dir, config.AuthFile), []byte(toml), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := config.LoadAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &server{cfgDir: dir, policy: p, userHeader: "X-Forwarded-User"}
}

const twoPeople = `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "responder"
`

// Every route this console mounts has been classified, one way or the other.
//
// This is the whole design: the gate reads a table, and a path missing from it is refused rather
// than allowed. That is only worth anything if adding a route forces somebody to decide — so the
// check is not "does the table exist" but "does it cover the handler list", which is the half that
// rots. The alternative, a check inside each handler, is a list nobody remembers to add to and the
// way you find out is that something was open.
func TestEveryRouteHasBeenClassified(t *testing.T) {
	s := &server{}
	var missing []string
	for path := range s.handlers() {
		if public[path] || openToRead[path] {
			continue
		}
		if _, ok := mayWrite[path]; !ok {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d routes are in none of public / openToRead / mayWrite: %s\n"+
			"Each is refused until somebody says what it needs — decide here, not when a person "+
			"is surprised by it.", len(missing), strings.Join(missing, " "))
	}
	// And nothing is classified twice, which would be two answers to one question.
	for path := range mayWrite {
		if public[path] {
			t.Errorf("%s is public AND needs a permission", path)
		}
	}
	for path := range openToRead {
		if public[path] {
			t.Errorf("%s is public AND read-gated", path)
		}
	}
}

// A console nobody has configured behaves exactly as it always did.
//
// This is the state every console is in until a gateway is put in front of one, and a permission
// model that changed anything here would be a login screen for a house with one occupant.
func TestAConsoleWithNobodyConfiguredIsUnchanged(t *testing.T) {
	s := withPolicy(t, "")
	if s.policy.Configured() {
		t.Fatal("an empty auth.toml configured somebody")
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/fleet"}, {http.MethodPost, "/submit"}, {http.MethodPost, "/shell"},
	} {
		var reached bool
		h := s.mayDo(c.path, func(w http.ResponseWriter, r *http.Request) { reached = true })
		h(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
		if !reached {
			t.Errorf("%s %s was refused on a console with nobody configured", c.method, c.path)
		}
	}
}

// A role is the whole answer: what they may do, and — for the routes that name a companion — which
// companions they may do it to.
func TestARoleDecidesWhatReachesTheHandler(t *testing.T) {
	s := withPolicy(t, twoPeople)
	try := func(who, method, path, target string) int {
		h := s.mayDo(path, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		q := path
		if target != "" {
			q += "?d=" + target
		}
		r := httptest.NewRequest(method, q, nil)
		if who != "" {
			r.Header.Set("X-Forwarded-User", who)
		}
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}
	for _, c := range []struct {
		what           string
		who, method, p string
		want           int
	}{
		{"an operator prompts", "kim@corp.com", http.MethodPost, "/submit", http.StatusNoContent},
		{"an operator runs a command", "kim@corp.com", http.MethodPost, "/shell", http.StatusNoContent},
		{"a responder answers", "lee@corp.com", http.MethodPost, "/answer", http.StatusNoContent},
		{"a responder stops a turn", "lee@corp.com", http.MethodPost, "/interrupt", http.StatusNoContent},
		{"a responder reads", "lee@corp.com", http.MethodGet, "/fleet", http.StatusNoContent},
		{"a responder prompts", "lee@corp.com", http.MethodPost, "/submit", http.StatusForbidden},
		{"a responder changes the model", "lee@corp.com", http.MethodPost, "/model", http.StatusForbidden},
		{"a responder runs a command", "lee@corp.com", http.MethodPost, "/shell", http.StatusForbidden},
		{"a stranger reads", "nobody@corp.com", http.MethodGet, "/fleet", http.StatusForbidden},
		// The one that matters most: a request nothing named. On a configured console it is not
		// the operator, it is nobody.
		{"an unnamed caller reads", "", http.MethodGet, "/fleet", http.StatusForbidden},
		{"an unnamed caller prompts", "", http.MethodPost, "/submit", http.StatusForbidden},
	} {
		if got := try(c.who, c.method, c.p, ""); got != c.want {
			t.Errorf("%s: %d, wanted %d", c.what, got, c.want)
		}
	}
}

// Somebody narrowed to two companions cannot act on a third.
func TestAPersonCanBeNarrowedToSomeCompanions(t *testing.T) {
	s := withPolicy(t, `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "responder"
companions = ["docs"]
`)
	try := func(target string) int {
		h := s.mayDo("/answer", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		r := httptest.NewRequest(http.MethodPost, "/answer?d=%2Fc%2Fdaemon-"+target+".sock", nil)
		r.Header.Set("X-Forwarded-User", "lee@corp.com")
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}
	if got := try("docs"); got != http.StatusNoContent {
		t.Errorf("the companion they were given answered %d", got)
	}
	if got := try("billing"); got != http.StatusForbidden {
		t.Errorf("a companion they were not given answered %d — the scope is not being read", got)
	}
}

// A route nobody classified is refused, and the refusal says the table is what is missing.
//
// Written as a test of the gate rather than of the table, because this is the behaviour the design
// rests on: the day somebody adds a route and forgets, it is shut and it says why.
func TestAnUnclassifiedRouteIsRefused(t *testing.T) {
	s := withPolicy(t, twoPeople)
	var reached bool
	h := s.mayDo("/something-new", func(w http.ResponseWriter, r *http.Request) { reached = true })
	r := httptest.NewRequest(http.MethodPost, "/something-new", nil)
	r.Header.Set("X-Forwarded-User", "kim@corp.com")
	w := httptest.NewRecorder()
	h(w, r)
	if reached {
		t.Fatal("an unclassified route reached its handler, and for an operator it would never be noticed")
	}
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "/something-new") {
		t.Errorf("the refusal does not name the route: %d %q", w.Code, w.Body.String())
	}
}

// The page is served to anybody. It is an empty shell that fetches everything through the routes
// above, and refusing it hands somebody a broken browser instead of a screen that can say what is
// wrong — which is also where a login page will have to live.
func TestThePageItselfIsServedToAnybody(t *testing.T) {
	s := withPolicy(t, twoPeople)
	for _, p := range []string{"/", "/vendor/material.js", "/i18n/language.en.json"} {
		var reached bool
		key := p
		if !public[key] {
			key = p[:strings.LastIndexByte(p, '/')+1] // the subtree route
		}
		h := s.mayDo(key, func(w http.ResponseWriter, r *http.Request) { reached = true })
		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
		if !reached {
			t.Errorf("%s was refused, so the browser gets nothing rather than an explanation", p)
		}
	}
}

// /me is what the page draws itself from, and it answers the same shape either way.
func TestMeSaysWhatThisPersonMayDo(t *testing.T) {
	s := withPolicy(t, twoPeople)
	read := func(who string) map[string]any {
		r := httptest.NewRequest(http.MethodGet, "/me", nil)
		if who != "" {
			r.Header.Set("X-Forwarded-User", who)
		}
		w := httptest.NewRecorder()
		s.me(w, r)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: %v (%s)", who, err, w.Body.String())
		}
		return out
	}
	lee := read("lee@corp.com")
	can, _ := lee["can"].([]any)
	if len(can) != 2 {
		t.Errorf("a responder may %v", can)
	}
	// An unnamed caller on a configured console may nothing — and says so as an empty list rather
	// than a missing field, so the page has one shape to read.
	none := read("")
	if c, ok := none["can"].([]any); !ok || len(c) != 0 {
		t.Errorf("an unnamed caller was told %v", none["can"])
	}
}

// A permission file that is present and wrong stops the console.
//
// Both ways of being wrong end somewhere worse than stopping: one that fails to parse would leave
// nobody able to do anything, and one naming a capability this build does not have would leave
// somebody believing they had granted something. The moment to find out is while they are looking
// at what they just wrote.
func TestABrokenPermissionFileIsRefusedRatherThanGuessedAt(t *testing.T) {
	for _, c := range []struct{ what, toml, say string }{
		{"a capability that does not exist", `
[roles.helper]
can = ["read", "deploy"]
[people."kim@corp.com"]
role = "operator"
`, "deploy"},
		{"a role nobody defined", `
[people."kim@corp.com"]
role = "wizard"
`, "wizard"},
		{"nobody who can grant a role", `
[people."lee@corp.com"]
role = "viewer"
`, string(auth.Admin)},
		{"not TOML at all", "[[[", "auth.toml"},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, config.AuthFile), []byte(c.toml), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := config.LoadAuth(dir)
		if err == nil {
			t.Errorf("%s: loaded without complaint", c.what)
			continue
		}
		if !strings.Contains(err.Error(), c.say) {
			t.Errorf("%s: the complaint does not name it: %v", c.what, err)
		}
	}
}

// The permission file is read from the console's own config directory and never from a workspace.
//
// config.toml is merged with a project's .magi/config.toml, so a repository could grant itself a
// role by checking one in. This is why the policy is a separate file, and the test is here because
// the day somebody "tidies" it into config.toml, everything still works — until a cloned
// repository owns the machine.
func TestAWorkspaceCannotGrantItselfARole(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "repo", ".magi")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, config.AuthFile), []byte(`
[people."attacker@example.com"]
role = "operator"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadAuth(dir) // the CONSOLE's directory, which has no auth.toml
	if err != nil {
		t.Fatal(err)
	}
	if p.Configured() {
		t.Fatal("a policy was picked up from a workspace")
	}
	if !p.Allows("attacker@example.com", auth.Admin, "") {
		t.Fatal("this console is not the unconfigured one — the fixture is wrong")
	}
}
