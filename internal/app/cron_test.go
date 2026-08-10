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

// jobsOf turns a fixed set into the source the scheduler reads. Most tests do not change the
// definitions mid-run, so a constant source is the honest fixture for them.
func jobsOf(m map[string]config.CronJob) func() map[string]config.CronJob {
	return func() map[string]config.CronJob { return m }
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

	s := newCronScheduler(a, wd, jobsOf(map[string]config.CronJob{
		"nightly": job("0 3 * * *", "audit"), // three in the morning, nine hours ago
	}), clk.now, rec.log)

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
	s := newCronScheduler(a, wd, jobsOf(map[string]config.CronJob{"hourly": job("@hourly", "check")}),
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
	s := newCronScheduler(a, wd, jobsOf(map[string]config.CronJob{"nightly": job("0 3 * * *", prompt)}),
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
	s := newCronScheduler(a, wd, jobsOf(map[string]config.CronJob{"slow": job("@hourly", "takes a while")}),
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

	s := newCronScheduler(a, wd, jobsOf(map[string]config.CronJob{
		"good":       job("@daily", "fine"),
		"bad-spec":   job("0 3 * *", "four fields"),
		"no-prompt":  job("@daily", "   "),
		"never":      job("0 0 30 2 *", "the 30th of February"),
		"switched":   off(job("@daily", "off for now")),
		"unknown-at": job("@fortnightly", "not a shorthand"),
	}), clk.now, rec.log)

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
		s := newCronScheduler(a, wd, jobsOf(jobs), clk.now, nil)
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
		a.RunCron(ctx, wd, jobsOf(map[string]config.CronJob{"x": job("@daily", "x")}), nil)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCron kept running after its context was cancelled")
	}
}

// A workspace with nothing armed still keeps the loop, because the schedule tool can arm something
// later and a loop that had already returned would leave that job never firing. This used to return
// at once, which was right when jobs could only come from a file read at startup.
func TestAnEmptyScheduleStillWaitsForOneToBeAdded(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.RunCron(ctx, wd, jobsOf(nil), nil)
	}()
	select {
	case <-done:
		t.Fatal("RunCron returned with no jobs armed; a job added later would never fire")
	case <-time.After(150 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCron did not stop when cancelled")
	}
}

// A job written while the daemon is running starts happening without a restart. Push, not poll: the
// schedule tool writes the definitions and then says so.
func TestAJobAddedWhileRunningIsPickedUp(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 0, 59, 0, 0, time.UTC)}

	defs := map[string]config.CronJob{}
	var mu sync.Mutex
	load := func() map[string]config.CronJob {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]config.CronJob{}
		for k, v := range defs {
			out[k] = v
		}
		return out
	}
	s := newCronScheduler(a, wd, load, clk.now, nil)
	if len(s.Jobs()) != 0 {
		t.Fatalf("armed %d jobs from an empty set", len(s.Jobs()))
	}

	mu.Lock()
	defs["fresh"] = job("@hourly", "the newly scheduled thing")
	mu.Unlock()
	s.reload()

	if len(s.Jobs()) != 1 {
		t.Fatalf("armed %d jobs after reload, want 1", len(s.Jobs()))
	}
	clk.advance(time.Minute) // 01:00
	s.tickOnce(context.Background())
	if got := cronSessions(t, a, wd, "fresh"); len(got) != 1 {
		t.Errorf("the added job fired %d times, want 1", len(got))
	}

	// And removing it stops it.
	mu.Lock()
	delete(defs, "fresh")
	mu.Unlock()
	s.reload()
	if len(s.Jobs()) != 0 {
		t.Errorf("a removed job is still armed: %+v", s.Jobs())
	}
}

// Editing one job must not push every other job's next run forward. The arming survives a reload.
func TestReloadKeepsWhenTheUntouchedJobsAreNextDue(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)}
	defs := map[string]config.CronJob{"steady": job("@hourly", "unchanged")}
	s := newCronScheduler(a, wd, func() map[string]config.CronJob { return defs }, clk.now, nil)
	firstDue := s.Jobs()[0].Due

	// Half an hour later somebody adds a second job. The first one is still due on the hour.
	clk.advance(30 * time.Minute)
	defs["other"] = job("@daily", "new")
	s.reload()

	for _, j := range s.Jobs() {
		if j.Name == "steady" && !j.Due.Equal(firstDue) {
			t.Errorf("an untouched job was re-armed from %s to %s by an unrelated edit", firstDue, j.Due)
		}
	}
	// But a job whose schedule changed IS re-armed against the new one.
	defs["steady"] = job("@daily", "unchanged")
	s.reload()
	for _, j := range s.Jobs() {
		if j.Name == "steady" && j.Due.Equal(firstDue) {
			t.Error("a job whose schedule changed kept its old next run")
		}
	}
}

// A broken job is reported once, not once a minute for as long as the daemon lives.
func TestABrokenJobIsReportedOnceNotForever(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)}
	rec := &recorder{}
	s := newCronScheduler(a, wd, jobsOf(map[string]config.CronJob{
		"typo": job("0 3 * *", "four fields"),
	}), clk.now, rec.log)

	for range 10 {
		s.reload()
	}
	if n := strings.Count(rec.all(), "typo"); n != 1 {
		t.Errorf("complained about the same broken job %d times, want 1:\n%s", n, rec.all())
	}
}

// A job does not fire while anything else is running in the workspace.
//
// The overlap check used to ask only whether that job's OWN last run was still going, which left
// the two cases that actually happen: two jobs due in the same minute, and a job due while
// somebody is attached and working. This is an agent that edits files — two turns at once in one
// tree are two writers with nothing coordinating them.
func TestAJobDoesNotFireWhileAnythingElseIsRunning(t *testing.T) {
	a, _ := newApp(t, &fakeLLM{}, Config{Model: session.ModelRef{Provider: "test", Model: "m"}})
	if sid, busy := a.somethingRunning(); busy {
		t.Fatalf("an idle app reported %s running", sid)
	}
	// A session with a turn in flight is what startRun leaves behind.
	a.mu.Lock()
	st := a.stateLocked("somebody-elses-work")
	st.cancel = func() {}
	a.mu.Unlock()

	sid, busy := a.somethingRunning()
	if !busy {
		t.Fatal("a running turn was invisible, so a job would have fired on top of it")
	}
	if sid != "somebody-elses-work" {
		t.Errorf("it named %q — the skip is only diagnosable if it says which session", sid)
	}
	// And it is ANY session, not one the caller has to name: the previous check took a session id
	// and so could only ever see the job asking.
	a.mu.Lock()
	a.stateLocked("somebody-elses-work").cancel = nil
	a.mu.Unlock()
	if sid, busy := a.somethingRunning(); busy {
		t.Errorf("a finished turn still reads as running: %s", sid)
	}
}
