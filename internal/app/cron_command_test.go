package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/port"
)

// A job either asks or runs. Both set is refused rather than ordered: there is no stated answer
// for which goes first, or whether the second happens when the first fails, and a rule invented
// at the edit site would be a contract nobody wrote down.
func TestAJobEitherAsksOrRuns(t *testing.T) {
	a := newTestApp(t)
	wd := t.TempDir()

	out, err := a.EditSchedule(wd, port.ScheduleChange{Action: "set", Name: "both",
		Schedule: "@daily", Prompt: "read the commits", Command: "make test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not both") {
		t.Fatalf("a job with a prompt AND a command is refused, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(wd, ".magi", "config.toml")); err == nil {
		t.Fatal("a refused job was written anyway")
	}

	// Neither is refused too — the message names both roads rather than only the older one.
	out, _ = a.EditSchedule(wd, port.ScheduleChange{Action: "set", Name: "empty", Schedule: "@daily"})
	if !strings.Contains(out, "command") || !strings.Contains(out, "prompt") {
		t.Fatalf("a job with neither must name both kinds, got %q", out)
	}
}

// Switching a job's kind CLEARS the other key.
//
// Left behind, the old key sits in the file beside the new one and the job is refused at its next
// arming as one that both asks and runs — an edit that looked like it worked breaks the job the
// next time it should have fired.
func TestSwitchingAJobsKindClearsTheOtherKey(t *testing.T) {
	a := newTestApp(t)
	wd := t.TempDir()
	if _, err := a.EditSchedule(wd, port.ScheduleChange{Action: "set", Name: "nightly",
		Schedule: "@daily", Prompt: "read the commits"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditSchedule(wd, port.ScheduleChange{Action: "set", Name: "nightly",
		Command: "make test"}); err != nil {
		t.Fatal(err)
	}
	jobs := a.ScheduledJobs(wd)
	if len(jobs) != 1 {
		t.Fatalf("one job, got %+v", jobs)
	}
	if jobs[0].Command != "make test" {
		t.Fatalf("the command did not land: %+v", jobs[0])
	}
	if jobs[0].Prompt != "" {
		t.Fatalf("the old prompt survived the switch — the job now asks AND runs: %q", jobs[0].Prompt)
	}
	if jobs[0].Problem != "" {
		t.Fatalf("the switched job cannot run: %q", jobs[0].Problem)
	}
}

// A command's bound is a length of time and more than zero.
//
// Zero is not "no limit": an unbounded command holds the workspace's only turn slot and every
// later firing is skipped behind it — the shape an unattended permission prompt had.
func TestACommandsTimeoutIsCheckedWhereTheTypistIsLooking(t *testing.T) {
	a := newTestApp(t)
	wd := t.TempDir()
	for _, c := range []struct{ bound, want string }{
		{"soon", "length of time"},
		{"0s", "more than zero"},
	} {
		out, err := a.EditSchedule(wd, port.ScheduleChange{Action: "set", Name: "nightly",
			Schedule: "@daily", Command: "make test", Timeout: c.bound})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("timeout %q: %q does not say %q", c.bound, out, c.want)
		}
	}
	if _, err := a.EditSchedule(wd, port.ScheduleChange{Action: "set", Name: "nightly",
		Schedule: "@daily", Command: "make test", Timeout: "20m"}); err != nil {
		t.Fatal(err)
	}
	if got := a.ScheduledJobs(wd); len(got) != 1 || got[0].Timeout != "20m" {
		t.Fatalf("a good bound is kept, got %+v", got)
	}
}

// Arming refuses what the file should never have held, and says which job.
func TestArmingRefusesAJobThatBothAsksAndRuns(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	clk := &fakeClock{t: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	rec := &recorder{}
	s := newCronScheduler(a, wd, jobsOf(map[string]config.CronJob{
		"both":     {Schedule: "@daily", Prompt: "ask", Command: "run"},
		"neither":  {Schedule: "@daily"},
		"badbound": {Schedule: "@daily", Command: "make", Timeout: "soon"},
	}), clk.now, rec.log)
	rec.mu.Lock()
	joined := strings.Join(rec.lines, "\n")
	rec.mu.Unlock()
	for _, want := range []string{"both", "neither", "badbound"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the job %q was dropped without saying so: %s", want, joined)
		}
	}
	if len(s.jobs) != 0 {
		t.Fatalf("a job that cannot run must not be armed, got %+v", s.jobs)
	}
}
