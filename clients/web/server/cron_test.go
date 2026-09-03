package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
