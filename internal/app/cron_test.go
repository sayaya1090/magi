package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// fakeClock is a clock the test moves by hand. Real time is not a dependency any of this needs:
// every question here is "given that it is now X, what fires", and waiting for X to actually
// arrive would make the suite slow and flaky in exchange for nothing.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// recorder collects what the scheduler said, so a test can assert that something was REPORTED and
// not merely that it did not happen. A job dropped in silence and a job dropped with a reason are
// very different things to the person who wrote it.
type recorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *recorder) log(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, s)
}

func (r *recorder) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
}

func job(schedule, prompt string) config.CronJob {
	return config.CronJob{Schedule: schedule, Prompt: prompt}
}

func off(j config.CronJob) config.CronJob {
	no := false
	j.Enabled = &no
	return j
}

// cronSessions returns the sessions a scheduled job opened, by reading who asked. This is the same
// route the editors use to show a job's last run, so the test exercises the record rather than
// inventing a second way to see it.
func cronSessions(t *testing.T, a *App, workdir, jobName string) []session.SessionID {
	t.Helper()
	metas, err := a.ListSessions(context.Background(), workdir)
	if err != nil {
		t.Fatal(err)
	}
	var out []session.SessionID
	for _, m := range metas {
		if name, ok := CronOriginName(m.Origin); ok && name == jobName {
			out = append(out, m.ID)
		}
	}
	return out
}

// waitAllIdle is waitIdle (fork_test.go) over the several sessions one job accumulates. Tests that
// want to prove a SCHEDULING rule need the overlap rule out of the way, or they cannot tell which
// of the two stopped a fire.
func waitAllIdle(t *testing.T, a *App, sids []session.SessionID) {
	t.Helper()
	for _, sid := range sids {
		waitIdle(t, a, sid)
	}
}

func TestAJobDoesNotFireForATimeThatAlreadyPassed(t *testing.T) {
	// The rule that stops a machine coming back from a weekend and running everything it missed.
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)} // noon
	rec := &recorder{}

	s := newCronScheduler(a, wd, map[string]config.CronJob{
		"nightly": job("0 3 * * *", "audit"), // three in the morning, nine hours ago
	}, clk.now, rec.log)

	if len(s.Jobs()) != 1 {
		t.Fatalf("armed %d jobs, want 1", len(s.Jobs()))
	}
	if want := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC); !s.Jobs()[0].Due.Equal(want) {
		t.Errorf("armed for %s, want tomorrow at %s", s.Jobs()[0].Due, want)
	}
	s.tickOnce(context.Background())
	if got := cronSessions(t, a, wd, "nightly"); len(got) != 0 {
		t.Errorf("fired %d times for a slot that had already gone by", len(got))
	}
}

func TestSleepingThroughFourSlotsFiresOnce(t *testing.T) {
	// The other half of no-catch-up: the laptop was shut, four hourly slots went by, and the answer
	// is one run — not four in the same second, all editing the same files.
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)}
	s := newCronScheduler(a, wd, map[string]config.CronJob{"hourly": job("@hourly", "check")},
		clk.now, nil)

	clk.set(time.Date(2026, 8, 10, 5, 30, 0, 0, time.UTC)) // woke up four hours later
	// Ticked several times at the SAME instant, which is what the daemon does while it works
	// through the backlog of wall-clock minutes. Re-arming from the missed slot instead of from now
	// would leave the job due on every one of these.
	//
	// Each tick waits for the previous run to finish first. Without that wait this test passed with
	// the re-arm rule broken: the second fire was being stopped by the overlap check instead, and
	// the test could not tell the two mechanisms apart. One test, one mechanism.
	for range 4 {
		s.tickOnce(context.Background())
		waitAllIdle(t, a, cronSessions(t, a, wd, "hourly"))
	}

	if got := cronSessions(t, a, wd, "hourly"); len(got) != 1 {
		t.Fatalf("fired %d times, want 1", len(got))
	}
	// And it is due at the next slot, not owed the three it missed.
	if want := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC); !s.Jobs()[0].Due.Equal(want) {
		t.Errorf("re-armed for %s, want %s", s.Jobs()[0].Due, want)
	}
}

func TestAFireOpensItsOwnSessionAndAsksThePromptVerbatim(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 2, 59, 0, 0, time.UTC)}
	const prompt = "walk yesterday's commits and report regression risk"
	s := newCronScheduler(a, wd, map[string]config.CronJob{"nightly": job("0 3 * * *", prompt)},
		clk.now, nil)

	clk.advance(time.Minute) // 03:00
	s.tickOnce(context.Background())

	sids := cronSessions(t, a, wd, "nightly")
	if len(sids) != 1 {
		t.Fatalf("opened %d sessions, want 1", len(sids))
	}
	evs, err := a.store.Read(context.Background(), sids[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		// Only an ActorUser prompt starts a top-level turn. A scheduled job that wrote a
		// system-kind prompt would open a session, put one message in it, and sit there.
		if e.Actor.Kind != event.ActorUser {
			t.Errorf("prompt actor kind is %q; nothing would have run", e.Actor.Kind)
		}
		if e.Actor.ID != "cron:nightly" {
			t.Errorf("prompt actor id is %q, want cron:nightly", e.Actor.ID)
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		for _, p := range d.Parts {
			if p.Kind == session.PartText {
				asked = append(asked, p.Text)
			}
		}
	}
	if len(asked) != 1 || asked[0] != prompt {
		t.Errorf("asked %q, want the prompt verbatim (%q)", asked, prompt)
	}
}

// Uses blockingLLM from spawn_test.go, which holds the first request open until released — exactly
// the shape needed to make a run still be running when the next slot comes round.
func TestAJobDoesNotStartAgainWhileItsLastRunIsStillGoing(t *testing.T) {
	llm := &blockingLLM{release: make(chan struct{}), started: make(chan struct{})}
	a, wd := newApp(t, llm, Config{})
	defer close(llm.release)

	clk := &fakeClock{t: time.Date(2026, 8, 10, 0, 59, 0, 0, time.UTC)}
	rec := &recorder{}
	s := newCronScheduler(a, wd, map[string]config.CronJob{"slow": job("@hourly", "takes a while")},
		clk.now, rec.log)

	clk.advance(time.Minute) // 01:00 — first fire
	s.tickOnce(context.Background())
	select {
	case <-llm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first run never reached the model")
	}

	clk.advance(time.Hour) // 02:00 — still going
	s.tickOnce(context.Background())

	if got := cronSessions(t, a, wd, "slow"); len(got) != 1 {
		t.Errorf("opened %d sessions, want 1 — the second slot should have been skipped", len(got))
	}
	if !strings.Contains(rec.all(), "skipped") {
		t.Errorf("skipped in silence; the report said:\n%s", rec.all())
	}
}

func TestAJobThatCannotRunIsDroppedOutLoud(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	rec := &recorder{}

	s := newCronScheduler(a, wd, map[string]config.CronJob{
		"good":       job("@daily", "fine"),
		"bad-spec":   job("0 3 * *", "four fields"),
		"no-prompt":  job("@daily", "   "),
		"never":      job("0 0 30 2 *", "the 30th of February"),
		"switched":   off(job("@daily", "off for now")),
		"unknown-at": job("@fortnightly", "not a shorthand"),
	}, clk.now, rec.log)

	if len(s.Jobs()) != 1 || s.Jobs()[0].Name != "good" {
		var names []string
		for _, j := range s.Jobs() {
			names = append(names, j.Name)
		}
		t.Fatalf("armed %v, want only [good]", names)
	}
	said := rec.all()
	// A job switched off on purpose needs no explanation; the other four are mistakes and the
	// person who made them has no other way to find out.
	for _, name := range []string{"bad-spec", "no-prompt", "never", "unknown-at"} {
		if !strings.Contains(said, name) {
			t.Errorf("%q was dropped without saying so:\n%s", name, said)
		}
	}
	if strings.Contains(said, "switched") {
		t.Errorf("a job turned off on purpose should not be reported as a problem:\n%s", said)
	}
}

func TestJobsAreArmedInTheSameOrderEveryRun(t *testing.T) {
	// Map iteration order would make two daemons on the same config disagree about which job goes
	// first within a minute, and make the startup lines shuffle between restarts.
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	jobs := map[string]config.CronJob{
		"zulu": job("@daily", "z"), "alpha": job("@daily", "a"), "mike": job("@daily", "m"),
	}
	want := []string{"alpha", "mike", "zulu"}
	for range 5 {
		s := newCronScheduler(a, wd, jobs, clk.now, nil)
		var got []string
		for _, j := range s.Jobs() {
			got = append(got, j.Name)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("armed %v, want %v", got, want)
		}
	}
}

func TestRunCronStopsWhenTheDaemonDoes(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.RunCron(ctx, wd, map[string]config.CronJob{"x": job("@daily", "x")}, nil)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCron kept running after its context was cancelled")
	}
}

func TestRunCronWithNothingArmedReturnsAtOnce(t *testing.T) {
	// A workspace with no jobs — the overwhelming majority — should not be left holding a timer.
	a, wd := newApp(t, &fakeLLM{}, Config{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.RunCron(context.Background(), wd, nil, nil)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCron blocked with no jobs armed")
	}
}
