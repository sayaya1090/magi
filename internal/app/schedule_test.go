package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/port"
)

func set(name, schedule, prompt string) port.ScheduleChange {
	return port.ScheduleChange{Action: "set", Name: name, Schedule: schedule, Prompt: prompt}
}

// jobsOnDisk reads the project config back, so the tests assert what was WRITTEN rather than what
// the method said it wrote.
func jobsOnDisk(t *testing.T, workdir string) map[string]config.CronJob {
	t.Helper()
	c, err := config.Load(filepath.Join(workdir, ".magi"))
	if err != nil {
		t.Fatal(err)
	}
	return c.Cron
}

func TestSchedulingAJobWritesItWhereItCanBeFound(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})

	out, err := a.EditSchedule(wd, set("nightly", "0 3 * * *", "audit yesterday's commits"))
	if err != nil {
		t.Fatal(err)
	}
	// The answer names the file. A standing job somebody cannot find is the failure this design is
	// arranged against, so the tool says where it put it.
	if !strings.Contains(out, filepath.Join(wd, ".magi", "config.toml")) {
		t.Errorf("the answer does not say where the job went: %q", out)
	}

	jobs := jobsOnDisk(t, wd)
	j, ok := jobs["nightly"]
	if !ok {
		t.Fatalf("nothing written: %+v", jobs)
	}
	if j.Schedule != "0 3 * * *" || j.Prompt != "audit yesterday's commits" {
		t.Errorf("wrote %+v", j)
	}
	// Absent means on. Writing enabled=true on every edit would fill the file with a line that says
	// what the default already says.
	if j.Enabled != nil {
		t.Errorf("enabled was written as %v; it should have been left out", *j.Enabled)
	}
	if !j.On() {
		t.Error("a job just created does not run")
	}
}

func TestEditingAJobKeepsThePartsNotMentioned(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	if _, err := a.EditSchedule(wd, set("watch", "@daily", "the original words")); err != nil {
		t.Fatal(err)
	}
	// New words, no schedule: the time should survive.
	if _, err := a.EditSchedule(wd, port.ScheduleChange{
		Action: "set", Name: "watch", Prompt: "different words",
	}); err != nil {
		t.Fatal(err)
	}
	j := jobsOnDisk(t, wd)["watch"]
	if j.Schedule != "@daily" {
		t.Errorf("schedule became %q after editing only the prompt", j.Schedule)
	}
	if j.Prompt != "different words" {
		t.Errorf("prompt is %q", j.Prompt)
	}

	// New schedule, no words: the words should survive.
	if _, err := a.EditSchedule(wd, port.ScheduleChange{
		Action: "set", Name: "watch", Schedule: "@hourly",
	}); err != nil {
		t.Fatal(err)
	}
	j = jobsOnDisk(t, wd)["watch"]
	if j.Schedule != "@hourly" || j.Prompt != "different words" {
		t.Errorf("got %+v", j)
	}
}

func TestSwitchingAJobOffKeepsIt(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	if _, err := a.EditSchedule(wd, set("weekly", "@weekly", "the report")); err != nil {
		t.Fatal(err)
	}
	no := false
	out, err := a.EditSchedule(wd, port.ScheduleChange{Action: "set", Name: "weekly", Enabled: &no})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "switched off") {
		t.Errorf("turning a job off is not said out loud: %q", out)
	}
	j, ok := jobsOnDisk(t, wd)["weekly"]
	if !ok {
		t.Fatal("switching off deleted the job")
	}
	if j.On() {
		t.Error("the job still runs")
	}
	if j.Prompt != "the report" {
		t.Errorf("switching off lost the prompt: %+v", j)
	}
}

func TestARefusalExplainsItselfRatherThanFailing(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	cases := []struct {
		what string
		c    port.ScheduleChange
		want string
	}{
		{"no name", set("", "@daily", "x"), "needs a name"},
		{"a dot in the name", set("a.b", "@daily", "x"), "cannot be a job name"},
		{"a space in the name", set("a b", "@daily", "x"), "cannot be a job name"},
		{"no schedule", port.ScheduleChange{Action: "set", Name: "x", Prompt: "y"}, "needs a schedule"},
		{"a broken schedule", set("x", "0 3 * *", "y"), "will not do"},
		{"a schedule that never comes round", set("x", "0 0 30 2 *", "y"), "never comes round"},
		{"no prompt", port.ScheduleChange{Action: "set", Name: "x", Schedule: "@daily"}, "needs a prompt"},
		{"an unknown action", port.ScheduleChange{Action: "frobnicate"}, "not something this can do"},
		{"removing what is not there", port.ScheduleChange{Action: "remove", Name: "ghost"}, "no job called"},
	}
	for _, c := range cases {
		out, err := a.EditSchedule(wd, c.c)
		// An error return would surface as a tool failure. The caller is a model deciding what to do
		// next, and it needs the reason, not a stack.
		if err != nil {
			t.Errorf("%s: returned an error instead of an explanation: %v", c.what, err)
			continue
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: said %q, wanted something containing %q", c.what, out, c.want)
		}
	}
	// And none of those wrote anything.
	if jobs := jobsOnDisk(t, wd); len(jobs) != 0 {
		t.Errorf("a refused edit still wrote: %+v", jobs)
	}
}

func TestRemovingAJobTakesItOutOfTheFile(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	if _, err := a.EditSchedule(wd, set("temp", "@daily", "for now")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditSchedule(wd, port.ScheduleChange{Action: "remove", Name: "temp"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := jobsOnDisk(t, wd)["temp"]; ok {
		t.Error("the job is still in the file")
	}
}

func TestListingSaysWhenEachOneNextRuns(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	if out, _ := a.EditSchedule(wd, port.ScheduleChange{Action: "list"}); !strings.Contains(out, "Nothing is scheduled") {
		t.Errorf("an empty workspace says %q", out)
	}
	if _, err := a.EditSchedule(wd, set("alpha", "@daily", "the first")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EditSchedule(wd, set("beta", "@hourly", "the second")); err != nil {
		t.Fatal(err)
	}
	out, err := a.EditSchedule(wd, port.ScheduleChange{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"alpha", "beta", "the first", "the second", "next "} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing has no %q:\n%s", want, out)
		}
	}
	// Sorted, so two reads of an unchanged schedule look the same.
	if strings.Index(out, "alpha") > strings.Index(out, "beta") {
		t.Errorf("the listing is not in name order:\n%s", out)
	}
}

// A job whose schedule stopped parsing — hand-edited, or written before a stricter parser — is
// named as broken rather than listed as if it were fine. It will never run, and the listing is the
// only place that can say so.
func TestAJobThatCanNoLongerRunIsMarkedInTheListing(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	dir := filepath.Join(wd, ".magi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[cron.hand-edited]\nschedule = \"0 3 * *\"\nprompt = \"four fields\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := a.EditSchedule(wd, port.ScheduleChange{Action: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BROKEN") || !strings.Contains(out, "never run") {
		t.Errorf("a job that cannot run is listed as if it could:\n%s", out)
	}
}
