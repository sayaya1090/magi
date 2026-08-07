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
	// The page is in there byte for byte. It is no longer the FIRST thing in the file — the
	// language seed goes ahead of it, exactly as the handler writes it for a real browser — so the
	// check is containment plus "nothing before it but the seed".
	// The page, with one transformation and no other: root-relative asset paths become relative,
	// because a project site lives under /<repo>/ and a leading slash escapes to the domain root.
	// Compared after applying the same rewrite to the original, so any OTHER difference still
	// fails — the demo must not become a second page.
	want := indexHTML
	for _, prefix := range []string{"/vendor/", "/font/", "/i18n/"} {
		want = strings.ReplaceAll(want, "'"+prefix, "'."+prefix)
		want = strings.ReplaceAll(want, `"`+prefix, `".`+prefix)
		want = strings.ReplaceAll(want, "url("+prefix, "url(."+prefix)
	}
	for _, file := range []string{"/icon.svg", "/manifest.webmanifest"} {
		want = strings.ReplaceAll(want, `"`+file+`"`, `".`+file+`"`)
	}
	at := strings.Index(got, want)
	if at < 0 {
		t.Fatal("the demo does not carry the page this binary serves, byte for byte")
	}
	// Nothing the browser loads may still be root-relative: under /<repo>/ that is a 404, and for
	// the module import a 404 is a blank page.
	for _, ref := range referencedPaths(got) {
		if strings.HasPrefix(ref, "/") {
			t.Errorf("%s is still root-relative — on a project site that resolves to the domain "+
				"root and 404s", ref)
		}
	}
	if before := got[:at]; !strings.HasPrefix(before, "<script>window.__LANG=") || strings.Count(before, "<script>") != 1 {
		t.Errorf("something other than the language seed was put ahead of the page:\n%s", before)
	}
	if !strings.Contains(got, "demo — the real page") {
		t.Error("the demo does not say it is one")
	}
	// EVERY path the page reaches for has to be in the directory. A missing font degrades the look;
	// a missing module means the script never runs and the page is blank — which is what shipped
	// the first time this page became a module and emitDemo still copied only the fonts.
	for _, ref := range referencedPaths(got) {
		if _, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(strings.TrimPrefix(ref, "."), "/"))); err != nil {
			t.Errorf("the page reaches for %s and the demo does not carry it: %v", ref, err)
		}
	}
	// And the language seed, or the first paint is a screen of dotted keys.
	if !strings.Contains(got, "window.__LANG=") {
		t.Error("the demo has no language seed, so it opens showing its own key names")
	}
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
		// /i18n is answered by prefix (any locale falls back to English), not by an exact key.
		if path == "/events" || path == "/i18n" {
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
		regexp.MustCompile(`fetch\('(/[a-z0-9]+)`),
		regexp.MustCompile(`fetchList\('(/[a-z0-9]+)`),
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

// referencedPaths is every root-relative path the page loads: module imports, scripts, styles,
// links and CSS url()s. The demo must carry all of them, because it is opened from a directory
// rather than served by the binary that embeds them.
func referencedPaths(page string) []string {
	seen := map[string]bool{}
	var out []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`from '(\.?/[^']+)'`),
		regexp.MustCompile(`(?:src|href)="(\.?/[^"]+)"`),
		regexp.MustCompile(`url\((\.?/[^)]+)\)`),
	} {
		for _, m := range re.FindAllStringSubmatch(page, -1) {
			p := strings.Trim(m[1], "'\"")
			// Routes the mock answers are not files; only what the BROWSER loads is.
			// A link with a query is navigation within the page, not a file to carry.
			if seen[p] || strings.Contains(p, "/i18n/") || p == "/" || strings.Contains(p, "?") {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
