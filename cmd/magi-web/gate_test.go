package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
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
	if !p.Allows("attacker@example.com", auth.Admin, "", "") {
		t.Fatal("this console is not the unconfigured one — the fixture is wrong")
	}
}

// A scope that leaves the LISTS whole hides nothing.
//
// The gate checks the companion a request names, and the routes that answer "what else is there"
// name none — so /fleet handed over every companion's name, workspace path, host and current task
// to somebody scoped to one of them, and /interventions handed over what people had SAID to each,
// verbatim. Those two are where a scope is read from: the board, the masthead count and the
// dispatch roster are all drawn from the fleet list.
func TestTheListsAreFilteredByScopeToo(t *testing.T) {
	f := newFleetFixture(t)
	wd := namedWorkdir(t, "docs")
	other := namedWorkdir(t, "billing")
	f.daemonAt(wd, "docs", true)
	f.daemonAt(other, "billing", true)
	f.session("docs", wd, "the docs work", 1, false)
	f.session("billing", other, "the billing work", 1, false)

	f.srv.userHeader = "X-Forwarded-User"
	p, err := config.LoadAuth(policyDir(t, `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "operator"
companions = ["docs"]
`))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy = p

	names := func(who string) []string {
		r := httptest.NewRequest(http.MethodGet, "/fleet", nil)
		r.Header.Set("X-Forwarded-User", who)
		w := httptest.NewRecorder()
		f.srv.fleet(w, r)
		var got []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v (%s)", who, err, w.Body.String())
		}
		var out []string
		for _, a := range got {
			out = append(out, a.Name)
		}
		sort.Strings(out)
		return out
	}
	if got := names("kim@corp.com"); len(got) != 2 {
		t.Errorf("an unscoped operator sees %v, wanted both", got)
	}
	if got := names("lee@corp.com"); len(got) != 1 || got[0] != "docs" {
		t.Errorf("somebody scoped to docs sees %v — the list is the scope's biggest hole", got)
	}
}

// A dispatch names its target in the BODY, after the gate has checked the one in the query.
//
// So the front door saw "docs" and the work went to "billing". A scope cannot be checked only
// where a route begins when the route chooses its subject later.
func TestADispatchCannotReachOutsideTheScope(t *testing.T) {
	f := newFleetFixture(t)
	wd := namedWorkdir(t, "docs")
	other := namedWorkdir(t, "billing")
	sock := f.daemonAt(wd, "docs", true)
	f.daemonAt(other, "billing", true)
	f.session("docs", wd, "the docs work", 1, false)
	f.session("billing", other, "the billing work", 1, false)

	f.srv.userHeader = "X-Forwarded-User"
	p, err := config.LoadAuth(policyDir(t, `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "operator"
companions = ["docs"]
`))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy = p

	// Addressed at docs — which lee may reach — and aimed at billing, which they may not.
	r := httptest.NewRequest(http.MethodPost, "/dispatch?d="+url.QueryEscape(sock),
		strings.NewReader(url.Values{"to": {"billing"}, "text": {"do this"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-User", "lee@corp.com")
	w := httptest.NewRecorder()
	f.srv.dispatch(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("the dispatch answered %d — it passed the front door as docs and acted as billing",
			w.Code)
	}
}

// policyDir writes an auth.toml and hands back the directory holding it.
func policyDir(t *testing.T, toml string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.AuthFile), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// namedWorkdir is a workspace whose BASE is the name a scope is written against.
//
// In production the two are the same fact: the socket is derived from the workdir and the fleet
// row's name is its base directory, so `companions = ["docs"]` matches both. A fixture that named
// the socket and the directory differently would let a filter pass while agreeing with nothing.
func namedWorkdir(t *testing.T, name string) string {
	t.Helper()
	d := filepath.Join(shortTempDir(t), name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

// The same hole, three more lists.
//
// /fleet and /interventions were filtered when the scope went in; these were not, and they are the
// lists that carry the most. /handoffs is who asked whom, the question verbatim and the answer.
// /mcp is how another workspace is wired — command line, arguments, the names of the variables it
// is handed. /skills is the rules another team wrote for its own companion. None of the three
// names a companion in the request, so none of them was ever seen by the gate at the door.
func TestTheRemainingListsAreFilteredByScope(t *testing.T) {
	f := newFleetFixture(t)
	docs := namedWorkdir(t, "docs")
	billing := namedWorkdir(t, "billing")
	f.daemonAt(docs, "docs", true)
	f.daemonAt(billing, "billing", true)
	// A question billing put to docs. The row that reports it names both ends.
	f.session("docs", docs, fleet.DispatchMark+"billing, asked\n\nreconcile the invoices", 1, true)
	f.session("billing", billing, "the billing work", 1, false)
	writeConfig(t, filepath.Join(docs, ".magi"), "[mcp.docsearch]\ncommand = \"docs-mcp\"\n")
	writeConfig(t, filepath.Join(billing, ".magi"), "[mcp.ledger]\ncommand = \"ledger-mcp\"\n")
	f.learn(t, filepath.Join(docs, ".magi", "experience"), "docs-rule", "how docs work")
	f.learn(t, filepath.Join(billing, ".magi", "experience"), "billing-rule", "how billing works")

	f.srv.userHeader = "X-Forwarded-User"
	p, err := config.LoadAuth(policyDir(t, `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "operator"
companions = ["docs"]
`))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy = p

	ask := func(who, path string, h http.HandlerFunc, into any) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-Forwarded-User", who)
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s as %s answered %d: %s", path, who, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("%s as %s: %v (%s)", path, who, err, w.Body.String())
		}
	}

	var all, mine []fleet.Handoff
	ask("kim@corp.com", "/handoffs", f.srv.handoffs, &all)
	ask("lee@corp.com", "/handoffs", f.srv.handoffs, &mine)
	if len(all) != 1 {
		t.Fatalf("the fixture wrote no handoff for an unscoped operator to see: %+v", all)
	}
	if len(mine) != 0 {
		t.Errorf("somebody scoped to docs read %q asked of docs by billing — the row names both ends",
			mine[0].Request)
	}

	var servers []mcpServer
	ask("lee@corp.com", "/mcp", f.srv.mcp, &servers)
	for _, m := range servers {
		if m.Companion == "billing" {
			t.Errorf("somebody scoped to docs was told billing runs %q", m.Command)
		}
	}

	var rules []storedSkill
	ask("lee@corp.com", "/skills", f.srv.skills, &rules)
	for _, k := range rules {
		if k.Companion == "billing" {
			t.Errorf("somebody scoped to docs was shown billing's rule %q", k.Name)
		}
	}
	// And the same three, whole, for somebody with no scope at all — a filter that answers nothing
	// passes every test above and is worse than the leak.
	ask("kim@corp.com", "/mcp", f.srv.mcp, &servers)
	ask("kim@corp.com", "/skills", f.srv.skills, &rules)
	if len(servers) != 2 || len(rules) != 2 {
		t.Errorf("an unscoped operator sees %d servers and %d rules, wanted both of each",
			len(servers), len(rules))
	}
}

// A schedule is a prompt with a clock on it.
//
// /cron is `configure` because a job is configuration and lives in a config file — and that is
// exactly what would make it the way around `prompt`. A role written as "may set the model and the
// servers, may not give the companion work" hands somebody a form that gives it work every morning
// at nine, and the audit line says configure.
func TestASchedulerCannotPromptForSomebodyWhoMayNot(t *testing.T) {
	f := newFleetFixture(t)
	wd := namedWorkdir(t, "docs")
	sock := f.daemonAt(wd, "docs", true)
	f.session("docs", wd, "the docs work", 1, true)

	f.srv.userHeader = "X-Forwarded-User"
	p, err := config.LoadAuth(policyDir(t, `
[roles.tuner]
can = ["read", "configure"]

[people."kim@corp.com"]
role = "operator"

[people."pat@corp.com"]
role = "tuner"
`))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy = p

	post := func(who, job string) *httptest.ResponseRecorder {
		body := url.Values{"name": {job}, "schedule": {"@daily"}, "prompt": {"audit yesterday"}}
		r := httptest.NewRequest(http.MethodPost, "/cron?d="+url.QueryEscape(sock),
			strings.NewReader(body.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Forwarded-User", who)
		w := httptest.NewRecorder()
		f.srv.cron(w, r)
		return w
	}
	if w := post("pat@corp.com", "nightly"); w.Code != http.StatusForbidden {
		t.Errorf("somebody who may configure but not prompt scheduled a prompt (%d): %s",
			w.Code, w.Body.String())
	}
	if w := post("kim@corp.com", "nightly"); w.Code != http.StatusOK {
		t.Errorf("an operator could not write a job (%d): %s", w.Code, w.Body.String())
	}
	// And configure still buys what it says it does: the list is a read.
	r := httptest.NewRequest(http.MethodGet, "/cron?d="+url.QueryEscape(sock), nil)
	r.Header.Set("X-Forwarded-User", "pat@corp.com")
	w := httptest.NewRecorder()
	f.srv.cron(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("the tuner cannot even read the schedule (%d): %s", w.Code, w.Body.String())
	}
}

// Auto-approval is answering, in advance and for everything.
//
// /permission is `configure`, and "how strictly does it ask" is configuration. But `allow` approves
// every tool call the companion will make and `auto` approves its edits, so a role written as "may
// set how it runs, may not tell it yes" could do in one post what /answer governs one call at a
// time. Tightening it is not the same act and stays where it was.
func TestLooseningApprovalNeedsTheAnswerCapability(t *testing.T) {
	f := newFleetFixture(t)
	wd := namedWorkdir(t, "docs")
	sock := f.daemonAt(wd, "docs", true)
	f.session("docs", wd, "the docs work", 1, true)

	f.srv.userHeader = "X-Forwarded-User"
	p, err := config.LoadAuth(policyDir(t, `
[roles.tuner]
can = ["read", "configure"]

[people."kim@corp.com"]
role = "operator"

[people."pat@corp.com"]
role = "tuner"
`))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy = p

	set := func(who, mode string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/permission?d="+url.QueryEscape(sock),
			strings.NewReader(url.Values{"mode": {mode}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Forwarded-User", who)
		w := httptest.NewRecorder()
		f.srv.permission(w, r)
		return w
	}
	for _, mode := range []string{"allow", "auto"} {
		if w := set("pat@corp.com", mode); w.Code != http.StatusForbidden {
			t.Errorf("somebody who may not answer set the mode to %s (%d): %s",
				mode, w.Code, w.Body.String())
		}
	}
	// Putting a person back in the loop is not a way around the person. These reach the daemon —
	// which this fixture does not run — so anything but a refusal is the assertion.
	for _, mode := range []string{"ask", "deny"} {
		if w := set("pat@corp.com", mode); w.Code == http.StatusForbidden {
			t.Errorf("tightening to %s was refused to somebody who may configure: %s",
				mode, w.Body.String())
		}
	}
	if w := set("kim@corp.com", "allow"); w.Code == http.StatusForbidden {
		t.Errorf("an operator was refused: %s", w.Body.String())
	}
}
