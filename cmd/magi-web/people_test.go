package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/auth"
)

func ask(t *testing.T, s *server, who, method string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if method == http.MethodPost {
		r = httptest.NewRequest(method, "/people", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, "/people", nil)
	}
	r.Header.Set("X-Forwarded-User", who)
	w := httptest.NewRecorder()
	s.routes()["/people"](w, r)
	return w
}

// `admin` finally means something, and it means this.
//
// The capability was defined, granted to the operator role, and required by the loader — and no
// route asked for it, so it granted nothing. Reading the list is admin's business too: it is who
// exists, what each may do and which companions they are narrowed to, which is the map somebody
// works from when they want more than they have.
func TestOnlyAnAdminSeesOrChangesWhoMayUseThisConsole(t *testing.T) {
	s := withPolicy(t, `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "responder"
`)
	if w := ask(t, s, "lee@corp.com", http.MethodGet, nil); w.Code != http.StatusForbidden {
		t.Errorf("a responder read the people list (%d)", w.Code)
	}
	if w := ask(t, s, "lee@corp.com", http.MethodPost,
		url.Values{"who": {"lee@corp.com"}, "role": {"operator"}}); w.Code != http.StatusForbidden {
		t.Errorf("a responder promoted themselves (%d): %s", w.Code, w.Body.String())
	}
	w := ask(t, s, "kim@corp.com", http.MethodGet, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("an admin could not read the list (%d): %s", w.Code, w.Body.String())
	}
	var got peopleAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.People) != 2 || got.People[0].Who != "kim@corp.com" {
		t.Fatalf("the list is %+v", got.People)
	}
	if !got.People[0].Me {
		t.Error("the admin's own row is not marked, so a screen cannot warn them about editing it")
	}
	if len(got.Roles) == 0 {
		t.Error("no roles were offered, so a screen has to guess what a role may be")
	}
}

// A change takes effect on this process, not on the next restart.
func TestAGrantAppliesWithoutARestart(t *testing.T) {
	s := withPolicy(t, `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "viewer"
`)
	if s.policy.Allows("lee@corp.com", auth.Prompt, "", "") {
		t.Fatal("the fixture is wrong: a viewer may already prompt")
	}
	w := ask(t, s, "kim@corp.com", http.MethodPost,
		url.Values{"who": {"lee@corp.com"}, "role": {"operator"}})
	if w.Code != http.StatusOK {
		t.Fatalf("the grant was refused (%d): %s", w.Code, w.Body.String())
	}
	if !s.policy.Allows("lee@corp.com", auth.Prompt, "", "") {
		t.Error("the console is still applying the policy it started with")
	}
	// And it is on disk, which is what a machine with no console still has.
	p, err := config.LoadAuth(s.cfgDir)
	if err != nil || p.People["lee@corp.com"].Role != "operator" {
		t.Errorf("the file says %+v (%v)", p.People, err)
	}
}

// The door cannot be locked from the inside.
func TestTheLastAdminCannotRemoveTheirOwnWayBack(t *testing.T) {
	s := withPolicy(t, `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "responder"
`)
	for _, form := range []url.Values{
		{"who": {"kim@corp.com"}, "role": {"viewer"}},
		{"who": {"kim@corp.com"}, "remove": {"1"}},
	} {
		w := ask(t, s, "kim@corp.com", http.MethodPost, form)
		if w.Code != http.StatusConflict {
			t.Errorf("%v answered %d: %s", form, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "admin") {
			t.Errorf("the refusal does not say what would be missing: %s", w.Body.String())
		}
	}
	if !s.policy.Allows("kim@corp.com", auth.Admin, "", "") {
		t.Error("the refused change was applied to the running policy anyway")
	}
}
