package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The demo is the page plus a mock, never a copy of the page.
//
// A generator with its own copy is a demo that drifts, and a drifted demo is worse than none: it is
// evidence about something that is not shipped.
func TestTheDemoIsThePageItself(t *testing.T) {
	dir := t.TempDir()
	if err := emitDemo(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, indexHTML) {
		t.Fatal("the demo does not open with the page this binary serves, byte for byte")
	}
	if !strings.Contains(got, "demo — the real page") {
		t.Error("the demo does not say it is one")
	}
	// The font the page asks for, at the path it asks for it — otherwise the demo falls back to a
	// system serif and stops being a fair look at the thing.
	for _, want := range []string{".nojekyll", "font"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s is missing: %v", want, err)
		}
	}
	fonts, err := os.ReadDir(filepath.Join(dir, "font"))
	if err != nil || len(fonts) == 0 {
		t.Errorf("no fonts were written: %v", err)
	}
	for _, f := range fonts {
		if !strings.HasSuffix(f.Name(), ".woff2") {
			t.Errorf("%s is not a font", f.Name())
		}
	}
}

// Every route the page fetches has an answer in the mock, and every answer is a route the page
// actually fetches. The first keeps a screen from being blank in the demo; the second keeps a
// fixture alive for a route that no longer exists.
func TestTheMockAnswersExactlyWhatThePageAsksFor(t *testing.T) {
	asked := map[string]bool{}
	for _, m := range fetchPathsIn(indexHTML) {
		asked[m] = true
	}
	if len(asked) < 5 {
		t.Fatalf("only found %d fetched paths — the scan has lost its subject: %v", len(asked), asked)
	}
	for path := range asked {
		if path == "/events" { // the transcript comes over EventSource, mocked separately
			continue
		}
		if !strings.Contains(demoScript, "'"+path+"'") {
			t.Errorf("the page fetches %s and the mock has no answer for it — that screen is blank "+
				"in the demo", path)
		}
	}
	for _, quoted := range mockRoutesIn(demoScript) {
		if !asked[quoted] {
			t.Errorf("the mock answers %s and the page never asks for it", quoted)
		}
	}
}

// fetchPathsIn finds the paths the page READS. Only those need a fixture: a POST is answered by the
// mock's "would have sent" branch, which is deliberate — a demo that silently accepts a delete
// teaches the wrong thing about the real console.
func fetchPathsIn(page string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`fetch\('(/[a-z]+)`),
		regexp.MustCompile(`fetchList\('(/[a-z]+)`),
	} {
		for _, m := range re.FindAllStringSubmatch(page, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// mockRoutesIn finds the routes the fixture answers.
func mockRoutesIn(script string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`\n    '(/[a-z]+)':`).FindAllStringSubmatch(script, -1) {
		out = append(out, m[1])
	}
	return out
}
