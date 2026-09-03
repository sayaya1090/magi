package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
)

// The real wiring for the cron write doors: a real App, a real config file on disk, daemonEngine
// itself. Every door in this tree gets one — a review once proved a door that was green against a
// fake engine could never succeed against the real one, and that shape is unreachable from a fake.
//
// What it walks: the change goes through the same App call the agent's own `schedule` tool makes,
// lands in the workspace's config.toml, and comes back out of the listing the read door renders.
func TestTheCronDoorsWriteTheRealConfig(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	wd := t.TempDir()
	d := daemonEngine{App: a, workdir: wd}

	if got := d.ScheduledHere(); len(got) != 0 {
		t.Fatalf("a fresh workspace has no standing work, got %+v", got)
	}

	msg, err := d.EditCron(daemon.CronEdit{
		Name: "nightly", Schedule: "0 3 * * *", Prompt: "read yesterday's commits"})
	if err != nil {
		t.Fatalf("writing a job: %v (%s)", err, msg)
	}
	// On disk, in the workspace's own file — not in a register this process holds.
	b, err := os.ReadFile(filepath.Join(wd, ".magi", "config.toml"))
	if err != nil {
		t.Fatalf("the job is written to the workspace's config: %v", err)
	}
	if !strings.Contains(string(b), "[cron.nightly]") {
		t.Fatalf("the name becomes a table header, file was:\n%s", b)
	}

	jobs := d.ScheduledHere()
	if len(jobs) != 1 || jobs[0].Name != "nightly" {
		t.Fatalf("the written job is listed, got %+v", jobs)
	}
	// The words travel back. This is the field the read door used to drop, and without it no
	// screen can offer to edit the job it is showing.
	if jobs[0].Prompt != "read yesterday's commits" {
		t.Fatalf("what the job asks comes back, got %q", jobs[0].Prompt)
	}
	if jobs[0].Problem != "" {
		t.Fatalf("a job with a good schedule has no problem, got %q", jobs[0].Problem)
	}

	// A schedule that will not parse is REFUSED, and the world is unchanged — which is how the
	// door tells a refusal from a success without reading the engine's prose.
	if _, err := d.EditCron(daemon.CronEdit{Name: "broken", Schedule: "not a crontab", Prompt: "x"}); err != nil {
		t.Fatalf("a bad schedule is a refusal, not a failure of the call: %v", err)
	}
	if got := d.ScheduledHere(); len(got) != 1 {
		t.Fatalf("a refused job is not written, listing is %+v", got)
	}

	// Enabled is three-valued: an edit that only changes the words must not touch the switch.
	off := false
	if _, err := d.EditCron(daemon.CronEdit{Name: "nightly", Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EditCron(daemon.CronEdit{Name: "nightly", Prompt: "read the commits and the diffs"}); err != nil {
		t.Fatal(err)
	}
	jobs = d.ScheduledHere()
	if len(jobs) != 1 || jobs[0].Enabled {
		t.Fatalf("changing the words switched the job back on: %+v", jobs)
	}
	if jobs[0].Prompt != "read the commits and the diffs" {
		t.Fatalf("the new words landed, got %q", jobs[0].Prompt)
	}

	if _, err := d.EditCron(daemon.CronEdit{Name: "nightly", Remove: true}); err != nil {
		t.Fatal(err)
	}
	if got := d.ScheduledHere(); len(got) != 0 {
		t.Fatalf("the removed job is gone, got %+v", got)
	}
}
