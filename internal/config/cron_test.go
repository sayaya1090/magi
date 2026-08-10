package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A job written into the file is meant to run. Enabled is a pointer for exactly this: with a plain
// bool, every hand-written job would arrive switched off, and the person who wrote it would have no
// way to tell that from a job that had never been read at all.
func TestAJobRunsUnlessSomebodySaysOtherwise(t *testing.T) {
	dir := t.TempDir()
	toml := `[cron.nightly-audit]
schedule = "0 3 * * *"
prompt = "walk yesterday's commits and report regression risk"

[cron.paused]
schedule = "@weekly"
prompt = "the weekly one, off for now"
enabled = false

[cron.explicitly-on]
schedule = "@hourly"
prompt = "on, said out loud"
enabled = true
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Cron) != 3 {
		t.Fatalf("parsed %d jobs, want 3: %+v", len(c.Cron), c.Cron)
	}

	j := c.Cron["nightly-audit"]
	if j.Schedule != "0 3 * * *" {
		t.Errorf("schedule = %q", j.Schedule)
	}
	if j.Prompt != "walk yesterday's commits and report regression risk" {
		t.Errorf("prompt = %q", j.Prompt)
	}
	if j.Enabled != nil {
		t.Errorf("an absent enabled should stay absent, got %v", *j.Enabled)
	}
	if !j.On() {
		t.Error("a job with no enabled key does not run; it should")
	}

	if c.Cron["paused"].On() {
		t.Error("enabled = false still runs")
	}
	if !c.Cron["explicitly-on"].On() {
		t.Error("enabled = true does not run")
	}
	// And the zero job, which is what a caller holds for a name nobody wrote.
	if !(CronJob{}).On() {
		t.Error("the zero CronJob should follow the same default")
	}
}

// The names become TOML table headers and are what the editors address a job by, so they have to
// survive the round trip intact — including the dashes people actually use.
func TestJobNamesSurviveTheFile(t *testing.T) {
	dir := t.TempDir()
	toml := `[cron.a-dashed-name]
schedule = "@daily"
prompt = "x"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Cron["a-dashed-name"]; !ok {
		t.Errorf("dashed name lost, have %v", c.Cron)
	}
}
