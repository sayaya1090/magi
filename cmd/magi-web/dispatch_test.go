package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// dispatchTeam stands up two companions that say what they are for, and one that does not.
func dispatchTeam(t *testing.T) (*fleetFixture, *recordingEngine, *recordingEngine) {
	t.Helper()
	f := newFleetFixture(t)
	design, api := &recordingEngine{}, &recordingEngine{}
	dwd, awd, pwd := shortTempDir(t), shortTempDir(t), shortTempDir(t)
	f.liveDaemonAs(t, dwd, "d", design, daemon.Identity{
		Name: "design", Role: "the design system: component specs and visual review"})
	f.liveDaemonAs(t, awd, "a", api, daemon.Identity{
		Name: "api", Role: "the billing API and its contracts"})
	f.liveDaemon(t, pwd, "plain", &recordingEngine{}) // no declaration at all
	f.session("d", dwd, "idle", 0, true)
	f.session("a", awd, "idle", 0, true)
	f.session("plain", pwd, "idle", 0, true)
	return f, design, api
}

// A request is addressed to what a companion IS, not to where it lives. Which machine and which
// directory the design work happens in is exactly what the person asking should not have to know —
// that is the difference between a list of processes and a team.
func TestWorkIsAddressedByWhatACompanionIsFor(t *testing.T) {
	f, design, api := dispatchTeam(t)

	// By role, in the words of whoever set it up — not the name.
	w := post(t, f.srv, f.srv.dispatch, "/dispatch", url.Values{
		"to": {"visual review"}, "text": {"spec the empty state for the fleet table"}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("dispatch by role replied %d: %s", w.Code, w.Body.String())
	}
	if got := design.seen(); len(got) != 1 || !strings.Contains(got[0], "empty state") {
		t.Fatalf("the design companion got %v", got)
	}
	if got := api.seen(); len(got) != 0 {
		t.Errorf("the api companion was also given the work: %v", got)
	}

	// By name, which is the exact address.
	if w := post(t, f.srv, f.srv.dispatch, "/dispatch", url.Values{
		"to": {"api"}, "text": {"add the idempotency key"}}); w.Code != http.StatusNoContent {
		t.Fatalf("dispatch by name replied %d: %s", w.Code, w.Body.String())
	}
	if got := api.seen(); len(got) != 1 || !strings.Contains(got[0], "idempotency") {
		t.Errorf("the api companion got %v", got)
	}

	// Verbatim, to the byte. Every recorded failure of handing work to another agent in this tree
	// began with somebody's words being rewritten on the way — a brief paraphrased until the exact
	// identifier it named was gone. So the assertion is equality, not "contains": a prefix nobody
	// asked for is the same defect starting.
	if got, want := design.seen()[0], "steer:spec the empty state for the fleet table"; got != want {
		t.Errorf("the request arrived as %q, want %q", got, want)
	}
}

// Two matches is an error, never a choice. Sending work to a companion the person did not mean is
// not recoverable by them noticing later — a turn will have run in somebody else's workspace.
func TestAnAmbiguousAddressRefusesInsteadOfPicking(t *testing.T) {
	f, design, api := dispatchTeam(t)

	w := post(t, f.srv, f.srv.dispatch, "/dispatch", url.Values{
		"to": {"the"}, "text": {"do something"}}) // matches both roles
	if w.Code != http.StatusConflict {
		t.Fatalf("an ambiguous address replied %d: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{"api", "design"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("the refusal does not say who matched: %s", w.Body.String())
		}
	}
	if len(design.seen())+len(api.seen()) != 0 {
		t.Error("an ambiguous dispatch sent the work anyway")
	}
}

// Nobody matched: the answer names the team, because the next thing the person does is address one
// of them and they cannot if they do not know who is there.
func TestAnAddressNobodyAnswersToNamesTheTeam(t *testing.T) {
	f, design, api := dispatchTeam(t)

	w := post(t, f.srv, f.srv.dispatch, "/dispatch", url.Values{
		"to": {"database"}, "text": {"add an index"}})
	if w.Code != http.StatusNotFound {
		t.Fatalf("an unknown address replied %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"design", "the design system", "api"} {
		if !strings.Contains(body, want) {
			t.Errorf("the roster does not mention %q: %s", want, body)
		}
	}
	if len(design.seen())+len(api.seen()) != 0 {
		t.Error("work was sent despite nobody matching")
	}
}

// A name beats a role. "design" is what one companion is called and a word another's role could
// well contain; the chosen address wins over the sentence.
func TestAChosenNameBeatsARoleThatMerelyMentionsIt(t *testing.T) {
	f := newFleetFixture(t)
	named, mentions := &recordingEngine{}, &recordingEngine{}
	nwd, mwd := shortTempDir(t), shortTempDir(t)
	f.liveDaemonAs(t, nwd, "n", named, daemon.Identity{Name: "design", Role: "component specs"})
	f.liveDaemonAs(t, mwd, "m", mentions, daemon.Identity{
		Name: "web", Role: "the web app, including anything the design system touches"})
	f.session("n", nwd, "idle", 0, true)
	f.session("m", mwd, "idle", 0, true)

	if w := post(t, f.srv, f.srv.dispatch, "/dispatch", url.Values{
		"to": {"design"}, "text": {"a spec"}}); w.Code != http.StatusNoContent {
		t.Fatalf("replied %d: %s", w.Code, w.Body.String())
	}
	if len(named.seen()) != 1 || len(mentions.seen()) != 0 {
		t.Errorf("named got %v, the one that merely mentions it got %v", named.seen(), mentions.seen())
	}
}

// A companion whose daemon is not running cannot be handed anything, and saying "sent" would be a
// lie somebody only discovers when the work never comes back.
func TestWorkIsNotHandedToACompanionThatIsNotRunning(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "gone", false) // a socket file with nobody listening
	f.session("gone", wd, "idle", 0, true)

	w := post(t, f.srv, f.srv.dispatch, "/dispatch", url.Values{
		"to": {filepath.Base(wd)}, "text": {"something"}})
	if w.Code != http.StatusConflict {
		t.Fatalf("dispatching to a stopped companion replied %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not running") {
		t.Errorf("the refusal does not say why: %s", w.Body.String())
	}
}
