package main

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
)

// A companion you may not act on and a companion that is not there are different answers.
//
// target() fails for two reasons — nothing resolves the name, or the caller is scoped away from
// what it resolved to — and eleven routes wrote the status for that failure themselves. Measured
// before this: 404 in seven of them and 400 in four, picked by which file the handler lived in.
// So the same refusal answered three different things across the console, and one of them (400,
// "your request was malformed") was never true of either cause.
//
// The front door already answers 403 for exactly this refusal (TestAPersonCanBeNarrowedToSome
// Companions, through mayDo). target()'s own scope check — which exists because mayDo waves an
// empty `d` through — did not.
func TestTargetStatusSeparatesNotYoursFromNotThere(t *testing.T) {
	if got := targetStatus(errNotYours); got != http.StatusForbidden {
		t.Errorf("a scope refusal answered %d, want 403", got)
	}
	// Wrapped, because a caller may add context and errors.Is is how that keeps working.
	if got := targetStatus(errors.New("x: " + errNotYours.Error())); got == http.StatusForbidden {
		t.Error("403 is being decided by the message text, not by the error — a look-alike passed")
	}
	if got := targetStatus(errors.Join(errNotYours, errors.New("while dialling"))); got != http.StatusForbidden {
		t.Errorf("a wrapped scope refusal answered %d, want 403", got)
	}
	if got := targetStatus(errors.New("no daemon in this directory")); got != http.StatusNotFound {
		t.Errorf("an absent companion answered %d, want 404", got)
	}
}

// And the seam every route now goes through gives those answers for real.
//
// The 404 half is driven end to end: a console with no companion of its own and a request naming
// none has nothing to resolve. The 403 half goes through a configured policy that scopes this
// person away from the name they asked for — the same shape gate_test uses for the front door.
func TestTargetOrAnswersTheCauseNotTheFile(t *testing.T) {
	// Nothing to name: 404, and the resolver's own words rather than a bare status.
	bare := &server{}
	w := httptest.NewRecorder()
	if _, ok := bare.targetOr(w, httptest.NewRequest(http.MethodGet, "/x", nil)); ok {
		t.Fatal("a console with no companion resolved one")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("no companion to name answered %d, want 404", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("the refusal said nothing at all")
	}

	// Scoped away from it: 403, not 404 and not 400.
	//
	// A short config dir, because the record has to sit next to a REAL socket file — Find globs
	// for `daemon-*.sock` and a record with no socket beside it is not published as far as it is
	// concerned — and a unix socket path is capped near 104 bytes, which the default temp name
	// nearly spends on its own.
	dir, derr := os.MkdirTemp("", "mgw")
	if derr != nil {
		t.Fatal(derr)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(dir, config.AuthFile), []byte(`
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "responder"
companions = ["docs"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	pol, perr := config.LoadAuth(dir)
	if perr != nil {
		t.Fatal(perr)
	}
	s := &server{cfgDir: dir, policy: pol, userHeader: "X-Forwarded-User"}

	sock := filepath.Join(daemon.SocketDir(dir), "daemon-billing.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		t.Fatal(err)
	}
	addr, aerr := net.ResolveUnixAddr("unix", sock)
	if aerr != nil {
		t.Fatal(aerr)
	}
	ln, lerr := net.ListenUnix("unix", addr)
	if lerr != nil {
		t.Fatal(lerr)
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if _, err := daemon.Publish(sock, "/w/billing", "s_b", daemon.Identity{Name: "billing"}); err != nil {
		t.Fatal(err)
	}
	// target() resolves the name BEFORE it checks scope, so without a record that resolves, the
	// answer would be 404 for the ordinary reason and this would pass saying nothing.
	if _, err := daemon.Find(dir, sock); err != nil {
		t.Fatalf("the fixture did not publish anything to be refused: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/x?d="+url.QueryEscape(sock), nil)
	r.Header.Set("X-Forwarded-User", "lee@corp.com")
	w2 := httptest.NewRecorder()
	if _, ok := s.targetOr(w2, r); ok {
		t.Fatal("a companion outside this person's scope resolved")
	}
	if w2.Code != http.StatusForbidden {
		t.Errorf("a companion they may not act on answered %d, want 403", w2.Code)
	}
}
