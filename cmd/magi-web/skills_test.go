package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/port"
)

func (f *fleetFixture) learn(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := expgit.New(dir).Propose(context.Background(), port.Contribution{
		Skills: []port.Skill{{Name: name, Description: desc, Body: desc + " — the long version"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *fleetFixture) inventory(t *testing.T) []storedSkill {
	t.Helper()
	w := httptest.NewRecorder()
	f.srv.skills(w, httptest.NewRequest(http.MethodGet, "/skills", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/skills answered %d: %s", w.Code, w.Body.String())
	}
	var out []storedSkill
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable: %v", err)
	}
	return out
}

// The tier boundary is the whole of context hygiene, and it is only as good as somebody's ability
// to see it. A rule sitting in the global tier reaches every prompt on every project, and after the
// day it was written nothing else in the system would ever mention it again.
func TestTheSkillsViewSaysWhichTierEachIsIn(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)

	f.learn(t, filepath.Join(f.cfgDir, "experience"), "commit-style", "commit messages carry the issue number")
	f.learn(t, filepath.Join(wd, ".magi", "experience"), "auth-service", "the auth service uses X")

	list := f.inventory(t)
	if len(list) != 2 {
		t.Fatalf("the inventory has %d entries: %+v", len(list), list)
	}
	// The crossing tier first: it is the one with reach, and the one worth reviewing.
	if list[0].Tier != "global" || !strings.Contains(list[0].Description, "issue number") {
		t.Errorf("the first entry is %+v", list[0])
	}
	if list[1].Tier != "project" || list[1].Companion != filepath.Base(wd) {
		t.Errorf("the project entry does not say whose it is: %+v", list[1])
	}
	// The header a governance decision is made on: how settled it is, and when it was last seen.
	if list[0].Observed < 1 || list[0].LastSeen == "" {
		t.Errorf("the entry carries no history: %+v", list[0])
	}
}

// A promoted rule that turned out to be wrong has to be removable. A store that only grows is one
// people stop promoting into, because the cost of a mistake is permanent.
func TestAWrongRuleCanBeForgotten(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	f.learn(t, filepath.Join(f.cfgDir, "experience"), "bad-idea", "always force push")
	f.learn(t, filepath.Join(wd, ".magi", "experience"), "local-thing", "this repo builds with make")

	if w := post(t, f.srv, f.srv.forgetSkill, "/forget", url.Values{
		"name": {"skill-bad-idea"}, "tier": {"global"}}); w.Code != http.StatusNoContent {
		t.Fatalf("forgetting a global rule replied %d: %s", w.Code, w.Body.String())
	}
	if w := post(t, f.srv, f.srv.forgetSkill, "/forget?d="+url.QueryEscape(sock), url.Values{
		"name": {"skill-local-thing"}, "tier": {"project"}}); w.Code != http.StatusNoContent {
		t.Fatalf("forgetting a project rule replied %d: %s", w.Code, w.Body.String())
	}
	if list := f.inventory(t); len(list) != 0 {
		t.Errorf("after forgetting both, the inventory still has %+v", list)
	}

	// A name that is not there says so rather than reporting success on a deletion that did not
	// happen — a supervisor who saw "done" and finds the rule still firing next week stops trusting
	// the button.
	if w := post(t, f.srv, f.srv.forgetSkill, "/forget", url.Values{
		"name": {"skill-never-existed"}, "tier": {"global"}}); w.Code != http.StatusNotFound {
		t.Errorf("forgetting nothing replied %d, want 404", w.Code)
	}
	// And a GET deletes nothing.
	if w := get(t, f.srv.forgetSkill, "/forget?name=x&tier=global"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /forget replied %d, want 405", w.Code)
	}
}

// A name from the page names an ENTRY, never a path. The store matches it against what it holds, so
// asking for something outside the tier reaches nothing.
func TestForgettingCannotReachOutsideTheTier(t *testing.T) {
	f := newFleetFixture(t)
	f.learn(t, filepath.Join(f.cfgDir, "experience"), "keeper", "worth keeping")
	for _, bad := range []string{"../../../etc/passwd", "../skills/skill-keeper", "skill-keeper.md"} {
		if w := post(t, f.srv, f.srv.forgetSkill, "/forget", url.Values{
			"name": {bad}, "tier": {"global"}}); w.Code != http.StatusNotFound {
			t.Errorf("%q replied %d, want 404", bad, w.Code)
		}
	}
	if list := f.inventory(t); len(list) != 1 {
		t.Errorf("something was removed by a path-shaped name: %+v", list)
	}
}

// A companion that published no workspace contributes no rules.
//
// The empty path is not harmless here: joined, it becomes ".magi/experience" RELATIVE to whatever
// directory the console was started in, so a record with a missing field would have the console
// listing its own working directory's rules under a companion named "." — and the forget button
// beside them would delete them.
func TestACompanionWithNoWorkspaceContributesNothing(t *testing.T) {
	f := newFleetFixture(t)
	f.learn(t, filepath.Join(f.cfgDir, "experience"), "keeper", "worth keeping")
	f.daemonAt("", "nowhere", true)

	// The rules that WOULD be picked up, sitting where a relative join would land.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(shortTempDir(t))
	defer func() { _ = os.Chdir(cwd) }()
	f.learn(t, filepath.Join(".magi", "experience"), "somebody-elses", "should not be listed")

	list := f.inventory(t)
	if len(list) != 1 || list[0].Tier != "global" {
		t.Fatalf("the console listed rules from a path it was never given: %+v", list)
	}
}
