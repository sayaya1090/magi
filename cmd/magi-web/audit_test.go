package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read is every line of the record, parsed.
func readAudit(t *testing.T, dir string) []entry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, auditName))
	if err != nil {
		t.Fatal(err)
	}
	var out []entry
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var e entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("a line of the record is not JSON: %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// serverWithAudit is a console whose record can be read back.
func serverWithAudit(t *testing.T) (*server, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := newAudit(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return &server{audit: a}, dir
}

// What changed something is written down; what only looked is not.
//
// The second half is what keeps the record readable: the page polls the fleet every three seconds
// and holds an event stream open, and a file where those outnumber the actions a thousand to one
// is a file nobody opens when they need it.
func TestTheRecordHoldsWhatChangedAndNotWhatLooked(t *testing.T) {
	s, dir := serverWithAudit(t)
	h := s.audited(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fleet", nil))
	r := httptest.NewRequest(http.MethodPost, "/submit?d=/tmp/agent.sock", nil)
	r.RemoteAddr = "127.0.0.1:51423"
	h(httptest.NewRecorder(), r)

	got := readAudit(t, dir)
	if len(got) != 1 {
		t.Fatalf("recorded %d requests, wanted only the one that changed something: %+v", len(got), got)
	}
	e := got[0]
	if e.Method != "POST" || e.Path != "/submit" {
		t.Errorf("the line does not say what was asked: %+v", e)
	}
	if e.Status != http.StatusNoContent {
		t.Errorf("the line says the answer was %d, not %d", e.Status, http.StatusNoContent)
	}
	if e.Agent != "/tmp/agent.sock" {
		t.Errorf("the line does not name the companion: %+v", e)
	}
	// The port is different on every connection and means nothing; the host is the fact.
	if e.From != "127.0.0.1" {
		t.Errorf("from is %q, wanted the host without the ephemeral port", e.From)
	}
	if e.At == "" {
		t.Error("the line has no time on it")
	}
}

// A refusal is the line somebody actually wants, and it never reaches a handler.
//
// The cross-site guard answers 403 before the route runs. Recorded from inside, this file would
// hold every ordinary action and none of the attempts — which is the wrong half.
func TestARefusedRequestIsRecorded(t *testing.T) {
	s, dir := serverWithAudit(t)
	var reached bool
	h := s.audited(sameSiteOnly(func(w http.ResponseWriter, r *http.Request) { reached = true }))

	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7777/mcp", nil)
	r.Host = "127.0.0.1:7777"
	r.Header.Set("Origin", "http://evil.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	h(httptest.NewRecorder(), r)

	if reached {
		t.Fatal("the guard let a cross-site POST through, which is a different bug")
	}
	got := readAudit(t, dir)
	if len(got) != 1 || got[0].Status != http.StatusForbidden {
		t.Fatalf("the refusal is not in the record: %+v", got)
	}
}

// Who, when there is a gateway in front to say so — and nothing invented when there is not.
//
// The header is named by the operator because every gateway spells it differently, and because a
// header this trusted by default would be one anybody could set.
func TestTheRecordNamesThePersonOnlyWhenAGatewaySaysSo(t *testing.T) {
	for _, c := range []struct {
		what   string
		header string
		set    map[string]string
		want   string
	}{
		{"a gateway that names them", "X-Forwarded-User",
			map[string]string{"X-Forwarded-User": "kim@example.com"}, "kim@example.com"},
		{"no gateway configured", "",
			map[string]string{"X-Forwarded-User": "kim@example.com"}, ""},
		{"a gateway that sent nothing", "X-Forwarded-User", nil, ""},
	} {
		s, dir := serverWithAudit(t)
		s.userHeader = c.header
		h := s.audited(func(w http.ResponseWriter, r *http.Request) {})
		r := httptest.NewRequest(http.MethodPost, "/interrupt", nil)
		for k, v := range c.set {
			r.Header.Set(k, v)
		}
		h(httptest.NewRecorder(), r)
		if got := readAudit(t, dir); got[0].Who != c.want {
			t.Errorf("%s: recorded who=%q, wanted %q", c.what, got[0].Who, c.want)
		}
	}
}

// The record says a prompt was submitted. It does not say what it said.
//
// The session log already holds every word, verbatim, with a time and an actor. A second copy here
// would be the whole conversation again in a file with different permissions and nothing that ever
// compacts it — and the question this file answers is who opened the door, not what was discussed
// on the other side of it.
func TestTheRecordDoesNotCopyWhatWasTyped(t *testing.T) {
	s, dir := serverWithAudit(t)
	h := s.audited(func(w http.ResponseWriter, r *http.Request) {})
	secret := "delete the production database"
	r := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader("text="+secret))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h(httptest.NewRecorder(), r)

	b, err := os.ReadFile(filepath.Join(dir, auditName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "production") {
		t.Errorf("the record copied the body: %s", b)
	}
}

// An implicit 200 is a 200. A handler that writes a body without a status is the ordinary shape of
// the JSON routes here, and a record that called those zero would make every successful action
// look like something that never answered.
func TestAHandlerThatJustWritesIsRecordedAsSuccess(t *testing.T) {
	s, dir := serverWithAudit(t)
	h := s.audited(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) })
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/skills", nil))
	if got := readAudit(t, dir); got[0].Status != http.StatusOK {
		t.Errorf("an implicit 200 was recorded as %d", got[0].Status)
	}
}

// Every route the server mounts goes through the record. It is the same wiring half the cross-site
// guard has a test for: the wrapper existing and the wrapper being ON everything are two facts, and
// the second is the one that rots when a route is added.
func TestEveryRouteIsRecorded(t *testing.T) {
	s, dir := serverWithAudit(t)
	table := s.routes()
	if len(table) == 0 {
		t.Fatal("no routes")
	}
	for path, h := range table {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7777"+path, nil)
		r.Host = "127.0.0.1:7777"
		r.Header.Set("Origin", "http://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site") // refused by the guard, so no handler runs
		h(httptest.NewRecorder(), r)
	}
	got := readAudit(t, dir)
	if len(got) != len(table) {
		seen := map[string]bool{}
		for _, e := range got {
			seen[e.Path] = true
		}
		var missing []string
		for path := range table {
			if !seen[path] {
				missing = append(missing, path)
			}
		}
		t.Errorf("%d of %d routes are not recorded: %s", len(missing), len(table), strings.Join(missing, " "))
	}
}

// A console with nowhere to write the record still serves. Being unable to keep the record is not
// a reason to withhold the page from the person in front of it — it is said at startup instead.
func TestAConsoleWithNoRecordStillWorks(t *testing.T) {
	s := &server{}
	var reached bool
	h := s.audited(func(w http.ResponseWriter, r *http.Request) { reached = true })
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/submit", nil))
	if !reached {
		t.Error("a console with no audit file refused the request")
	}
}
