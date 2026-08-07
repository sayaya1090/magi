package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Promotion is the point of the whole supervision loop: one person holds several companions only
// if each intervention permanently removes future ones, and a correction that stays in a
// transcript is one you will give again next week.
func TestPromotingACorrectionWritesARule(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "port the handler", 2, false)

	// global: the craft tier, shared by every companion this person runs.
	if w := post(t, f.srv, f.srv.promote, "/promote", url.Values{
		"text": {"run the tests before you say it is done"}, "scope": {"global"}}); w.Code != http.StatusNoContent {
		t.Fatalf("/promote global replied %d: %s", w.Code, w.Body.String())
	}
	globalFile := findSkill(t, filepath.Join(f.cfgDir, "experience"))
	if !strings.Contains(globalFile, "run the tests before you say it is done") {
		t.Errorf("the global rule does not carry the words: %q", globalFile)
	}
	// Where it came from, because a rule whose origin is lost cannot be argued with later.
	if !strings.Contains(globalFile, "promoted by the person supervising") {
		t.Errorf("the rule does not say where it came from: %q", globalFile)
	}

	// project: stays with that companion, and with its repo.
	if w := post(t, f.srv, f.srv.promote, "/promote?d="+url.QueryEscape(sock), url.Values{
		"text": {"do not touch vendor/"}, "scope": {"project"}}); w.Code != http.StatusNoContent {
		t.Fatalf("/promote project replied %d: %s", w.Code, w.Body.String())
	}
	projectFile := findSkill(t, filepath.Join(wd, ".magi", "experience"))
	if !strings.Contains(projectFile, "do not touch vendor/") {
		t.Errorf("the project rule does not carry the words: %q", projectFile)
	}
	// And the two tiers stayed apart — that separation is the only thing keeping one project's
	// truth out of another's prompts.
	if strings.Contains(projectFile, "run the tests") {
		t.Error("the global rule leaked into the project tier")
	}
}

// The tier is named, never defaulted: the two differ in whether the lesson crosses the companion
// boundary, and a default would make that crossing happen by omission.
func TestPromotionRefusesWhatItCannotPlace(t *testing.T) {
	f := newFleetFixture(t)
	for _, bad := range []url.Values{
		{"text": {"something"}},                          // no scope
		{"text": {"something"}, "scope": {"everywhere"}}, // not a tier
		{"scope": {"global"}},                            // nothing to promote
		{"text": {"   "}, "scope": {"global"}},
	} {
		if w := post(t, f.srv, f.srv.promote, "/promote", bad); w.Code != http.StatusBadRequest {
			t.Errorf("%v replied %d, want 400", bad, w.Code)
		}
	}
	// With a real companion named, so the tier check is the ONLY thing that can refuse it: without
	// this the test passed for the wrong reason — a missing ?d= made the project branch fail and
	// the scope check could be deleted without anything noticing.
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	for _, bad := range []string{"", "everywhere", "GLOBAL", "project "} {
		w := post(t, f.srv, f.srv.promote, "/promote?d="+url.QueryEscape(sock),
			url.Values{"text": {"something"}, "scope": {bad}})
		if w.Code != http.StatusBadRequest {
			t.Errorf("scope %q replied %d, want 400", bad, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(wd, ".magi")); !os.IsNotExist(err) {
		t.Error("a rule with an unnamed tier was written anyway")
	}

	// A project rule with no companion has nowhere to go, and saying so beats writing it somewhere.
	w := post(t, f.srv, f.srv.promote, "/promote", url.Values{"text": {"x"}, "scope": {"project"}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("a project rule with no companion replied %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "companion") {
		t.Errorf("the refusal does not say what is missing: %q", w.Body.String())
	}
	// And a GET writes nothing: this puts a file on disk.
	if w := get(t, f.srv.promote, "/promote?text=x&scope=global"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /promote replied %d, want 405", w.Code)
	}
}

// The project tier's path comes from the PUBLISHED companion and never from the request. A page
// that could name the directory could name any directory, and this process writes files there.
func TestAProjectRuleCannotBeAimedAtAnArbitraryDirectory(t *testing.T) {
	f := newFleetFixture(t)
	victim := shortTempDir(t)
	w := post(t, f.srv, f.srv.promote, "/promote?d="+url.QueryEscape(victim), url.Values{
		"text": {"anything"}, "scope": {"project"}})
	if w.Code == http.StatusNoContent {
		t.Fatal("a directory nobody published was accepted as a companion")
	}
	if _, err := os.Stat(filepath.Join(victim, ".magi")); !os.IsNotExist(err) {
		t.Error("a file was written into a directory that is not a published companion")
	}
}

// findSkill returns the one skill file under a store directory.
func findSkill(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatalf("no skills under %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d skills under %s, want one", len(entries), dir)
	}
	b, err := os.ReadFile(filepath.Join(dir, "skills", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	return entries[0].Name() + "\n" + string(b)
}

// A rule keeps its own letters, whatever they are: a name mangled to nothing is a directory of
// files called rule, rule-1, rule-2.
func TestARulesFileNameIsReadable(t *testing.T) {
	for text, want := range map[string]string{
		"run the tests before you say it is done": "run-the-tests-before-you-say",
		"테스트 먼저 돌려라":                              "테스트-먼저-돌려라",
		"!!!":                                     "rule",
		"Do NOT touch vendor/":                    "do-not-touch-vendor",
	} {
		if got := ruleName(text); got != want {
			t.Errorf("ruleName(%q) = %q, want %q", text, got, want)
		}
	}
}
