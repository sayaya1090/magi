package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// cronModel is a Model over a real workspace directory, so the screen writes a real config file and
// the test reads it back. Nothing here is stubbed: the point of these is that pressing keys changes
// what is on disk, and a fake engine would only prove the keys reached a fake.
func cronModel(t *testing.T) (*Model, string) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	a := app.New(store, stubLLM{}, builtin.Default(), bus.New(), nil, app.Config{Permission: "allow"})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), a, nil, sid, "m", wd, true, "")
	m.width, m.height = 100, 40
	return &m, wd
}

func onDisk(t *testing.T, wd string) map[string]config.CronJob {
	t.Helper()
	c, err := config.Load(filepath.Join(wd, ".magi"))
	if err != nil {
		t.Fatal(err)
	}
	return c.Cron
}

// press sends keys to the /cron screen and fails if one is not consumed — a key that falls through
// this screen types into the composer behind it, which is how a "d" meant for delete ends up in
// somebody's next prompt.
func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, handled := m.handleCronKey(key(k)); !handled {
			t.Fatalf("the /cron screen did not consume %q; it would have typed into the composer", k)
		}
	}
}

func TestAJobCanBeWrittenFromTheTerminal(t *testing.T) {
	m, wd := cronModel(t)
	m.openCron()

	press(t, m, "n") // new
	for _, r := range "audit" {
		press(t, m, string(r))
	}
	press(t, m, "tab")
	for _, r := range "@daily" {
		press(t, m, string(r))
	}
	press(t, m, "tab")
	for _, r := range "check the logs" {
		press(t, m, string(r))
	}
	press(t, m, "enter")

	j, ok := onDisk(t, wd)["audit"]
	if !ok {
		t.Fatalf("nothing was written: %+v", onDisk(t, wd))
	}
	if j.Schedule != "@daily" || j.Prompt != "check the logs" {
		t.Errorf("wrote %+v", j)
	}
	// And the screen shows it, having gone back to the list.
	if m.cronEditing {
		t.Error("the editor is still open after saving")
	}
	if v := m.cronView(); !strings.Contains(v, "audit") {
		t.Errorf("the new job is not in the list:\n%s", v)
	}
}

func TestSpaceSwitchesAJobOffAndOnInTheFile(t *testing.T) {
	m, wd := cronModel(t)
	if _, err := m.app.EditSchedule(wd, setChange("nightly", "@daily", "x")); err != nil {
		t.Fatal(err)
	}
	m.openCron()

	press(t, m, " ")
	if onDisk(t, wd)["nightly"].On() {
		t.Error("space did not switch the job off on disk")
	}
	press(t, m, " ")
	if !onDisk(t, wd)["nightly"].On() {
		t.Error("space did not switch it back on")
	}
}

// Deleting asks first. It is the one thing on this screen that cannot be undone from this screen.
func TestDeletingAJobAsksFirst(t *testing.T) {
	m, wd := cronModel(t)
	if _, err := m.app.EditSchedule(wd, setChange("doomed", "@daily", "x")); err != nil {
		t.Fatal(err)
	}
	m.openCron()

	press(t, m, "d")
	if _, ok := onDisk(t, wd)["doomed"]; !ok {
		t.Fatal("the job went without being confirmed")
	}
	if !strings.Contains(m.cronView(), "delete?") {
		t.Errorf("nothing on screen says a confirmation is waiting:\n%s", m.cronView())
	}
	press(t, m, "n") // anything but y
	if _, ok := onDisk(t, wd)["doomed"]; !ok {
		t.Fatal("declining the confirmation still deleted it")
	}

	press(t, m, "d", "y")
	if _, ok := onDisk(t, wd)["doomed"]; ok {
		t.Error("confirming did not delete it")
	}
}

// A refusal from the engine has to reach the screen. These are the sentences that say WHICH part of
// a crontab line was wrong, and a screen that dropped them would leave a person retyping blind.
func TestARefusedEditIsShownOnScreen(t *testing.T) {
	m, wd := cronModel(t)
	m.openCron()

	press(t, m, "n")
	for _, r := range "broken" {
		press(t, m, string(r))
	}
	press(t, m, "tab")
	for _, r := range "0 3 * *" { // four fields
		press(t, m, string(r))
	}
	press(t, m, "tab")
	for _, r := range "never runs" {
		press(t, m, string(r))
	}
	press(t, m, "enter")

	if len(onDisk(t, wd)) != 0 {
		t.Errorf("a refused schedule was written anyway: %+v", onDisk(t, wd))
	}
	if !strings.Contains(m.cronView(), "5 fields") {
		t.Errorf("the reason is not on screen:\n%s", m.cronView())
	}
}

// The editor says what the line being typed means, while it is being typed. A crontab line is not
// readable at a glance and the moment to discover a mistake is before saving.
func TestTheEditorExplainsTheScheduleAsItIsTyped(t *testing.T) {
	m, _ := cronModel(t)
	m.openCron()
	press(t, m, "n")
	press(t, m, "tab") // to the schedule field

	for _, r := range "@daily" {
		press(t, m, string(r))
	}
	// Matched on the explanation, not on the word "next" anywhere in the view: the hint line reads
	// "tab next field", so a bare Contains(v, "next ") is true whatever the schedule says. It was,
	// and the assertion proved nothing.
	if v := m.cronView(); !strings.Contains(v, "next Mon") && !strings.Contains(v, "next Tue") &&
		!strings.Contains(v, "next Wed") && !strings.Contains(v, "next Thu") &&
		!strings.Contains(v, "next Fri") && !strings.Contains(v, "next Sat") &&
		!strings.Contains(v, "next Sun") {
		t.Errorf("a valid schedule is not explained with when it would fire:\n%s", v)
	}
	// Now make it invalid and check the explanation changes rather than going stale.
	for _, r := range "xx" {
		press(t, m, string(r))
	}
	v := m.cronView()
	if !strings.Contains(v, "unknown shorthand") {
		t.Errorf("an unparseable schedule is not explained:\n%s", v)
	}
	for _, day := range []string{"next Mon", "next Tue", "next Wed", "next Thu", "next Fri", "next Sat", "next Sun"} {
		if strings.Contains(v, day) {
			t.Errorf("an unparseable schedule still shows a next run (%s):\n%s", day, v)
		}
	}
}

// A job that can never run is the one the list MUST mark: it is switched on, it looks ordinary, and
// nothing will ever mention it again.
func TestAJobThatCanNeverRunIsMarkedInTheList(t *testing.T) {
	m, wd := cronModel(t)
	dir := filepath.Join(wd, ".magi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[cron.bad]\nschedule = \"0 3 * *\"\nprompt = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openCron()
	if v := m.cronView(); !strings.Contains(v, "never runs") {
		t.Errorf("a job that cannot run is listed as if it could:\n%s", v)
	}
}

func TestAnEmptyScheduleSaysHowToAddOne(t *testing.T) {
	m, _ := cronModel(t)
	m.openCron()
	v := m.cronView()
	if !strings.Contains(v, "nothing scheduled") {
		t.Errorf("an empty list does not say it is empty:\n%s", v)
	}
	if !strings.Contains(v, "n to add") {
		t.Errorf("an empty list does not say how to add one:\n%s", v)
	}
}

// Escape from the editor must not save. Half a typed schedule written to the file would be a job
// that never runs, created by cancelling.
func TestEscapingTheEditorWritesNothing(t *testing.T) {
	m, wd := cronModel(t)
	m.openCron()
	press(t, m, "n")
	for _, r := range "half" {
		press(t, m, string(r))
	}
	press(t, m, "esc")
	if len(onDisk(t, wd)) != 0 {
		t.Errorf("cancelling wrote: %+v", onDisk(t, wd))
	}
	if m.cronEditing {
		t.Error("escape left the editor open")
	}
}

// setChange is the "create this job" edit, spelled once.
func setChange(name, schedule, prompt string) port.ScheduleChange {
	return port.ScheduleChange{Action: "set", Name: name, Schedule: schedule, Prompt: prompt}
}

// The resume picker searches what was SAID, not just the titles.
//
// A title is the first thing that was asked. The reason somebody is looking a week later is usually
// that the thing they want was said in the middle — a picker that could only match titles was
// answering an easier question than the one being put to it.
func TestTheResumePickerSearchesInsideSessions(t *testing.T) {
	m, wd := cronModel(t)
	older := writeSession(t, m, wd, "set up the postgres container", "the vacuum threshold needs raising")
	writeSession(t, m, wd, "unrelated work", "nothing to do with databases")

	m.handleResume(nil)
	// Three: the two written here plus the empty one the model was built around.
	opened := len(m.resumeList)
	if opened < 2 {
		t.Fatalf("the picker opened with %d sessions", opened)
	}
	for _, r := range "vacuum" {
		if _, handled := m.handleKey(key(string(r))); !handled {
			t.Fatalf("the picker did not consume %q", r)
		}
	}
	if len(m.resumeList) != 1 || m.resumeList[0].ID != older {
		t.Fatalf("filtering on a word from the MIDDLE of a session gave %d rows", len(m.resumeList))
	}
	// And the row says why it matched, since the title does not contain the word.
	if why := m.resumeWhy[older]; !strings.Contains(why, "vacuum") {
		t.Errorf("the matching turn is not shown: %q", why)
	}
	v := m.resumeView()
	if !strings.Contains(v, "search") || !strings.Contains(v, "vacuum") {
		t.Errorf("the query is not on screen:\n%s", v)
	}

	// Escape clears the search before it closes the picker.
	m.handleKey(key("esc"))
	if !m.resuming {
		t.Error("esc closed the picker instead of clearing the search")
	}
	if len(m.resumeList) != opened {
		t.Errorf("clearing the search left %d rows, want the %d it opened with", len(m.resumeList), opened)
	}
}

// A search that matched nothing says so. Otherwise the list empties under the keystrokes and the
// screen goes back to its title, which is what a picker with no sessions at all looks like.
func TestASearchThatMatchesNothingSaysSo(t *testing.T) {
	m, wd := cronModel(t)
	writeSession(t, m, wd, "some work", "and a reply")
	m.handleResume(nil)
	for _, r := range "zzqq" {
		m.handleKey(key(string(r)))
	}
	v := m.resumeView()
	if !strings.Contains(v, "nothing in") {
		t.Errorf("an empty result does not say it is empty:\n%s", v)
	}
	if !strings.Contains(v, "backspace") {
		t.Errorf("an empty result does not say how to get back:\n%s", v)
	}
}

// writeSession puts a session on disk with one TURN per prompt, and returns its id.
//
// One turn each, not one turn holding everything: the unit the search ranks is a turn, so a fixture
// that put two subjects in one would be testing a shape the real logs do not have.
func writeSession(t *testing.T, m *Model, wd string, prompts ...string) session.SessionID {
	t.Helper()
	ctx := context.Background()
	a, ok := m.app.(*app.App)
	if !ok {
		t.Fatal("the test model is not over a real App")
	}
	sid, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: wd, Actor: event.Actor{Kind: event.ActorUser, ID: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range prompts {
		if err := a.SeedForTest(ctx, sid, p, fmt.Sprintf("answering %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	return sid
}
