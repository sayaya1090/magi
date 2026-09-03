package main

import (
	"context"
	"github.com/sayaya1090/magi/internal/config"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A cron job's name becomes a [cron.<name>] TOML header the same way a profile's does, so it is held
// to the same bare-key allowlist. This pins that cronWrite actually applies it — a newline name (the
// audit's finding) would otherwise split the header and leave the target companion's config.toml
// unparseable, and cron, unlike /profiles, is not shared-console-refused, so the guard matters more.
func TestACronNameThatWouldBreakTheFileIsRefused(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	q := "/cron?d=" + url.QueryEscape(sock)

	for _, name := range []string{"foo\nbar", "a.b", "my job", "a,b", "café", ""} {
		w := post(t, f.srv, f.srv.cron, q, url.Values{
			"name": {name}, "schedule": {"@daily"}, "prompt": {"do the thing"}})
		if w.Code != http.StatusBadRequest {
			t.Errorf("cron name %q: answered %d, want 400 (%s)", name, w.Code, w.Body.String())
		}
	}
	// A valid bare-key name is not over-rejected on the name check (it may fail later for other
	// reasons, but not with the name 400).
	w := post(t, f.srv, f.srv.cron, q, url.Values{
		"name": {"nightly-sweep_2"}, "schedule": {"@daily"}, "prompt": {"do the thing"}})
	if w.Code == http.StatusBadRequest {
		t.Errorf("a valid bare-key cron name was refused: %s", w.Body.String())
	}
}

// cronDoorEngine is a real daemon on the other end of the socket: it speaks the protocol and
// answers the cron doors, so this test can prove the console ASKS IT rather than writing the file.
type cronDoorEngine struct {
	mu   sync.Mutex
	jobs []app.ScheduledJobInfo
	got  []daemon.CronEdit
}

func (e *cronDoorEngine) Submit(context.Context, command.SubmitPrompt) error { return nil }
func (e *cronDoorEngine) Steer(context.Context, command.SubmitPrompt) error  { return nil }
func (e *cronDoorEngine) Interrupt(context.Context, command.Interrupt) error { return nil }
func (e *cronDoorEngine) RespondPermission(context.Context, command.RespondPermission) error {
	return nil
}
func (e *cronDoorEngine) RespondQuestion(context.Context, command.RespondQuestion) error { return nil }
func (e *cronDoorEngine) Waiting(session.SessionID) (app.Ask, bool)                      { return app.Ask{}, false }
func (e *cronDoorEngine) Doing(session.SessionID) (string, bool)                         { return "", false }

// About is what the handshake needs: without it `Hello` fails and the console cannot learn which
// doors are there, so it falls back to writing the file — which is what this test caught.
func (e *cronDoorEngine) About() string { return "a test companion" }

func (e *cronDoorEngine) ScheduledHere() []app.ScheduledJobInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]app.ScheduledJobInfo(nil), e.jobs...)
}

func (e *cronDoorEngine) EditCron(c daemon.CronEdit) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, c)
	if c.Remove {
		e.jobs = nil
		return "removed " + c.Name, nil
	}
	e.jobs = []app.ScheduledJobInfo{{Name: c.Name, Schedule: c.Schedule, Prompt: c.Prompt, Enabled: true}}
	return "set " + c.Name, nil
}

// **The daemon writes it when there is one.** This console knows how to compose config.toml — it
// has to, for a companion that is not running — but a second writer is a second copy of where a
// workspace's file lives, how a name becomes a table header and which of the three config layers
// wins. So when the far side advertises the doors, the edit goes through them and this process
// writes nothing.
//
// The handshake is what decides. Without it a transport failure and a refusal arrive as the same
// error, and this console reported one as the other — measured: a test daemon that predates these
// doors answered an EOF and a valid job name came back to the person as refused.
func TestTheConsoleAsksTheDaemonToWriteTheSchedule(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := filepath.Join(f.cfgDir, "daemon-door.sock")
	d, err := daemon.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	eng := &cronDoorEngine{}
	go func() { _ = d.Serve(context.Background(), eng) }()
	unpublish, err := daemon.Publish(sock, wd, "door", daemon.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpublish)

	q := "/cron?d=" + url.QueryEscape(sock)
	w := post(t, f.srv, f.srv.cron, q, url.Values{
		"name": {"nightly"}, "schedule": {"@daily"}, "prompt": {"read yesterday's commits"}})
	if w.Code != http.StatusOK {
		t.Fatalf("the write went through the door: %d %s", w.Code, w.Body.String())
	}
	eng.mu.Lock()
	got := append([]daemon.CronEdit(nil), eng.got...)
	eng.mu.Unlock()
	if len(got) != 1 || got[0].Name != "nightly" || got[0].Prompt != "read yesterday's commits" {
		t.Fatalf("the daemon was asked to write it, got %+v (body: %s)", got, w.Body.String())
	}
	// And nothing was composed here: the file this process would have written does not exist.
	if _, err := os.Stat(filepath.Join(wd, ".magi", "config.toml")); err == nil {
		t.Fatal("the console wrote the file itself while a daemon was there to own it")
	}

	// Removal takes the same road.
	w = post(t, f.srv, f.srv.cron, q, url.Values{"name": {"nightly"}, "delete": {"1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("the removal went through the door: %d %s", w.Code, w.Body.String())
	}
	eng.mu.Lock()
	last := eng.got[len(eng.got)-1]
	eng.mu.Unlock()
	if !last.Remove || last.Name != "nightly" {
		t.Fatalf("the removal crossed as %+v", last)
	}
}

// A scheduled COMMAND is the one kind of job this console can write that runs outside the tool
// permission gate — writing it is the approval. Three things have to hold, and each of them was
// wrong before the daemon learned command jobs: the console can write one at all, switching a job
// from one kind to the other clears the key it is no longer using, and `prompt` alone does not buy
// it. The last one matters most: a role written as "may give the companion work" would otherwise
// be a role that can run anything on that machine every morning at nine.
func TestAScheduledCommandIsWrittenAndNeedsMoreThanPrompt(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	// A dead daemon: a published record with nothing behind it. That is the case this path exists
	// for — the console composes the file itself because there is no owner running to ask.
	q := "/cron?d=" + url.QueryEscape(f.daemonAt(wd, "api", false))
	f.session("api", wd, "x", 1, false)
	path := filepath.Join(wd, ".magi", "config.toml")
	read := func() map[string]config.CronJob {
		t.Helper()
		c, err := config.Load(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		return c.Cron
	}

	// No daemon here on purpose: this is the path that exists for when one is down, and it is the
	// path that writes the file, so it is where a stale opposite key would survive.
	w := post(t, f.srv, f.srv.cron, q, url.Values{
		"name": {"build"}, "schedule": {"@daily"}, "command": {"make all"}, "timeout": {"20m"}})
	if w.Code != http.StatusOK {
		t.Fatalf("writing a command job: %d %s", w.Code, w.Body.String())
	}
	if j := read()["build"]; j.Command != "make all" || j.Timeout != "20m" || j.Prompt != "" {
		t.Fatalf("the command job as written: %+v", j)
	}

	// Switch it to the other kind. Both keys are written every time for this reason — leaving the
	// command behind would make a job that asks AND runs, the one shape the daemon refuses to arm.
	w = post(t, f.srv, f.srv.cron, q, url.Values{
		"name": {"build"}, "prompt": {"summarise last night"}})
	if w.Code != http.StatusOK {
		t.Fatalf("switching kinds: %d %s", w.Code, w.Body.String())
	}
	if j := read()["build"]; j.Command != "" || j.Prompt != "summarise last night" {
		t.Fatalf("switching to a prompt left the command behind: %+v", j)
	}

	// Both at once is refused rather than ordered — there is no stated answer for which goes first.
	w = post(t, f.srv, f.srv.cron, q, url.Values{
		"name": {"build"}, "prompt": {"ask"}, "command": {"run"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a job that both asks and runs: %d %s", w.Code, w.Body.String())
	}

	// And the capability. This role may give the companion work; it may not run a shell.
	authDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(authDir, config.AuthFile), []byte(`
[roles.tasker]
can = ["read", "prompt"]

[people."kim@corp.com"]
role = "tasker"

[people."boss@corp.com"]
role = "operator"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadAuth(authDir)
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy, f.srv.userHeader = p, "X-Forwarded-User"
	for _, c := range []struct {
		who  string
		want int
	}{{"kim@corp.com", http.StatusForbidden}, {"boss@corp.com", http.StatusOK}} {
		w := postAs(t, f.srv.cron, q, c.who, url.Values{
			"name": {"sweep"}, "schedule": {"@daily"}, "command": {"make clean"}})
		if w.Code != c.want {
			t.Errorf("%s writing a scheduled command: %d, want %d (%s)", c.who, w.Code, c.want, w.Body.String())
		}
	}
	// The same person may still schedule a prompt — the refusal is about the shell, not the clock.
	if w := postAs(t, f.srv.cron, q, "kim@corp.com", url.Values{
		"name": {"ask"}, "schedule": {"@daily"}, "prompt": {"how did last night go"}}); w.Code != http.StatusOK {
		t.Errorf("a prompt job was refused to someone who may prompt: %d %s", w.Code, w.Body.String())
	}
}

// postAs is post with a signed-in person — the console reads who from a header its front door set.
func postAs(t *testing.T, h http.HandlerFunc, path, who string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-User", who)
	w := httptest.NewRecorder()
	h(w, r)
	return w
}
