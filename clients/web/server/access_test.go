package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/auth"
)

func ask(t *testing.T, s *server, who, method string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if method == http.MethodPost {
		r = httptest.NewRequest(method, "/access", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, "/access", nil)
	}
	r.Header.Set("X-Forwarded-User", who)
	w := httptest.NewRecorder()
	s.routes()["/access"](w, r)
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
	var got accessAnswer
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
	// And the list says WHOSE it is. A console watching three machines draws this screen three
	// times the same way, and the list governs exactly one of them — the account this process runs
	// as, reading the directory it was pointed at. Without the name the roster reads as the fleet's.
	if !strings.Contains(got.Instance.Who, "@") {
		t.Errorf("the list does not name the account and machine it governs: %q", got.Instance.Who)
	}
	if got.Instance.ConfigDir != s.cfgDir {
		t.Errorf("the list says it came from %q and it was read from %q", got.Instance.ConfigDir, s.cfgDir)
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

// The refusal above is the right answer to a request nobody should have been able to make.
//
// A screen can only decline to offer a control if it is told, and this list was not telling it:
// it said whether a policy exists and never whether the console can tell who a request is from.
// So the button was drawn, a name was typed into the dialog, the dialog closed, and the reason
// arrived as a status note clipped at 80 characters — the half that says what to do about it cut
// off. Measured live against a console started without -user-header.
func TestTheListSaysWhetherThisConsoleCanNameAnybody(t *testing.T) {
	s := withPolicy(t, "") // nobody configured, so everybody reaching it is the operator
	s.userHeader = ""

	read := func() accessAnswer {
		t.Helper()
		w := ask(t, s, "", http.MethodGet, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("the list answered %d: %s", w.Code, w.Body.String())
		}
		var got accessAnswer
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); got.Named {
		t.Error("a console with nothing in front of it says it can name people, so the screen " +
			"offers a button whose only answer is 409")
	}
	s.userHeader = "X-Forwarded-User"
	if got := read(); !got.Named {
		t.Error("a console behind a gateway says it cannot name anybody, so the screen hides the " +
			"one control that would make it a console with a policy")
	}
}

// Adding the first person is what switches the gate on, so it is refused where that would lock
// the door on the way in.
//
// A console with no gateway in front of it cannot name anybody. With nobody listed that is fine —
// one operator, no policy — and the moment somebody is listed every request becomes an unnamed
// one, including the screen that just did it. The startup check refuses to boot such a console on
// the next run; this is the same refusal a moment earlier, while there is still somebody to tell.
func TestTheFirstPersonIsRefusedOnAConsoleThatCannotNameAnybody(t *testing.T) {
	s := withPolicy(t, "") // nobody configured
	s.userHeader = ""      // and nothing in front to say who is asking

	w := ask(t, s, "", http.MethodPost, url.Values{"who": {"kim@corp.com"}, "role": {"operator"}})
	if w.Code != http.StatusConflict {
		t.Fatalf("it answered %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "-user-header") {
		t.Errorf("the refusal does not say what is missing: %s", w.Body.String())
	}
	if s.policy.Configured() {
		t.Error("the policy was switched on anyway")
	}
	// With a gateway named, the same call goes through: this is about the console being able to
	// name people, not about who is asking.
	s.userHeader = "X-Forwarded-User"
	if w := ask(t, s, "", http.MethodPost,
		url.Values{"who": {"kim@corp.com"}, "role": {"operator"}}); w.Code != http.StatusOK {
		t.Errorf("the first person could not be added (%d): %s", w.Code, w.Body.String())
	}
}

// An unclaimed console says whose name to grant, once.
//
// It cannot work out that the person behind the gateway's header is the one who started it — the
// daemon recorded a uid and the header carries a name, and nothing joins the two. So it says the
// name where the person who CAN join them will see it: a terminal on that machine.
func TestAnUnclaimedConsoleSaysHowToClaimIt(t *testing.T) {
	var said strings.Builder
	log.SetOutput(&said)
	defer log.SetOutput(os.Stderr)

	s := withPolicy(t, "") // nobody configured
	s.userHeader = "X-Forwarded-User"
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/fleet", nil)
		r.Header.Set("X-Forwarded-User", "lee@corp.com")
		s.claimHint(r)
	}
	got := said.String()
	if !strings.Contains(got, "lee@corp.com") || !strings.Contains(got, "--grant") {
		t.Errorf("the note does not say the name or the command:\n%s", got)
	}
	if strings.Count(got, "--grant") != 1 {
		t.Errorf("it said it %d times; it is a note, not an alarm:\n%s", strings.Count(got, "--grant"), got)
	}

	// And a console that HAS a policy has nothing to claim, so it says nothing.
	said.Reset()
	s2 := withPolicy(t, twoPeople)
	s2.userHeader = "X-Forwarded-User"
	r := httptest.NewRequest(http.MethodGet, "/fleet", nil)
	r.Header.Set("X-Forwarded-User", "kim@corp.com")
	s2.claimHint(r)
	if said.Len() != 0 {
		t.Errorf("a configured console offered to be claimed:\n%s", said.String())
	}
}
