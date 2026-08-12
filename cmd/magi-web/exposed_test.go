package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A console more people than the operator can reach stops being a way to run commands on the
// machine.
//
// Both of these run something the CALLER chose, outside the permission policy an agent's own tool
// calls go through: /shell directly, and /mcp by writing a command line a daemon spawns at
// startup. Behind an authenticating proxy the caller is no longer necessarily the person who owns
// the machine, and the second one is easy to miss precisely because it looks like a settings form.
func TestASharedConsoleRefusesToRunThingsChosenByTheCaller(t *testing.T) {
	for _, c := range []struct {
		what  string
		path  string
		form  url.Values
		route func(*server) http.HandlerFunc
	}{
		{"a shell command", "/shell", url.Values{"cmd": {"whoami"}},
			func(s *server) http.HandlerFunc { return s.shell }},
		{"an MCP server", "/mcp", url.Values{"name": {"pwned"}, "command": {"/bin/sh"}},
			func(s *server) http.HandlerFunc { return s.mcp }},
	} {
		open := &server{}
		shared := &server{exposed: true}

		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c.route(shared)(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: a shared console answered %d, not a refusal", c.what, w.Code)
		}
		// The person on the other end has to be able to tell a policy from a fault, and to find
		// the switch. Without the flag's name the next move is to go looking for a bug.
		if !strings.Contains(w.Body.String(), "-exposed") {
			t.Errorf("%s: the refusal does not say what turned it off: %q", c.what, w.Body.String())
		}

		// And the ordinary console is untouched: refused for want of a companion to act on, which
		// is a 404 or a 502 from further in, but never the shared-console 403.
		w2 := httptest.NewRecorder()
		r2 := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.form.Encode()))
		r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c.route(open)(w2, r2)
		if w2.Code == http.StatusForbidden && strings.Contains(w2.Body.String(), "-exposed") {
			t.Errorf("%s: an ordinary console refused it as if it were shared", c.what)
		}
	}
}

// Reading is not writing. A shared console that hid what the daemons already run would invite
// somebody to add a second copy of a server that is right there.
func TestASharedConsoleStillShowsWhatIsRunning(t *testing.T) {
	s := &server{exposed: true, cfgDir: t.TempDir()}
	w := httptest.NewRecorder()
	s.mcp(w, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if w.Code == http.StatusForbidden {
		t.Fatalf("the list was refused on a shared console: %s", w.Body.String())
	}
}

// Federation on a shared console would let whoever the gateway admits act as the operator on
// another machine — magi-web forwards on the operator's own tunnel and has no credential of its
// own to narrow it with. Refused at startup, where it can still be a sentence rather than a
// surprise.
func TestASharedConsoleCannotAlsoFederate(t *testing.T) {
	peers := []peer{{Name: "mini", Base: "http://127.0.0.1:7778"}}
	if err := exposedAllows(true, peers); err == nil {
		t.Fatal("-exposed with -peer was accepted")
	} else {
		if !strings.Contains(err.Error(), "mini") {
			t.Errorf("the refusal does not name the peer: %v", err)
		}
		if !strings.Contains(err.Error(), "-exposed") || !strings.Contains(err.Error(), "-peer") {
			t.Errorf("the refusal does not name both flags: %v", err)
		}
	}
	if err := exposedAllows(false, peers); err != nil {
		t.Errorf("federation alone was refused: %v", err)
	}
	if err := exposedAllows(true, nil); err != nil {
		t.Errorf("a shared console with no peers was refused: %v", err)
	}
}

// Every route that changes something on the machine itself is either behind the shared-console
// refusal or is deliberately not — and the deliberate ones are listed here, so adding a route that
// spawns a process is a decision somebody makes on purpose rather than a default.
//
// The list is the point. Without it this check would be "some routes refuse", which is true of any
// number of them, including one.
func TestTheSharedRefusalCoversTheRoutesThatSpawnProcesses(t *testing.T) {
	shared := &server{exposed: true}
	// What is refused, by the path a caller uses.
	mustRefuse := map[string]http.HandlerFunc{"/shell": shared.shell, "/mcp": shared.mcp}
	for path, h := range mustRefuse {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, nil)
		h(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s does not refuse on a shared console (%d)", path, w.Code)
		}
	}
	// And what is deliberately NOT refused, with the reason: these reach the machine only through
	// the agent, which applies the permission policy the operator configured. They fail here for
	// want of a companion to act on — a 404 or a 502 from further in — and what this asserts is
	// that none of them fails with the shared-console refusal. Somebody adding it to one of these
	// turns a usable shared console into a read-only page, and would do it while making something
	// else safer.
	for path, h := range map[string]http.HandlerFunc{
		"/submit": shared.submit, "/answer": shared.answer, "/permission": shared.permission,
		"/cron": shared.cron, "/skills": shared.skills, "/dispatch": shared.dispatch,
	} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), "-exposed") {
			t.Errorf("%s is refused on a shared console, which leaves the page unable to do the "+
				"thing it is for; if that is intended, move it into the list above", path)
		}
	}
}

// A shared console will not start without a certificate.
//
// The operator chose authentication in magi rather than a proxy in front, and that decision brings
// this one with it: whatever identifies a person crosses this connection, and in plaintext it is
// on the wire. Loopback does not save it — the port is reached through something forwarding to it.
func TestASharedConsoleWillNotStartInPlaintext(t *testing.T) {
	if err := exposedHasTLS(true, "", ""); err == nil {
		t.Error("a shared console started with nothing to encrypt it")
	} else if !strings.Contains(err.Error(), "-tls-cert") {
		t.Errorf("the refusal does not say what to add: %v", err)
	}
	if err := exposedHasTLS(true, "cert.pem", ""); err == nil {
		t.Error("a certificate with no key was accepted")
	}
	if err := exposedHasTLS(true, "cert.pem", "key.pem"); err != nil {
		t.Errorf("a shared console with both was refused: %v", err)
	}
	// The ordinary console keeps working with neither: one operator on their own machine, over a
	// tunnel they made, gains nothing from a certificate they would have to invent.
	if err := exposedHasTLS(false, "", ""); err != nil {
		t.Errorf("an unshared console was made to produce a certificate: %v", err)
	}
	// But half a pair is a mistake either way — it would serve plaintext while somebody believed
	// otherwise.
	if err := exposedHasTLS(false, "cert.pem", ""); err == nil {
		t.Error("half a certificate pair was accepted on an unshared console")
	}
}
