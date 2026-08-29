package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConsole gives a fixture's server an assembled console to serve.
//
// A test binary has none. The console is compiled by gradle and put in place by CI — see
// cmd/magi-web/console/README.md — so `go test` runs against the placeholder that only keeps the
// directory alive. That absence is a real state and it is checked below; everything else about
// serving a console needs one to serve, and this writes it.
func withConsole(t *testing.T, s *server, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ui, err := consoleTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.ui, s.consoleDir, s.consolePage = ui, dir, consolePage(ui)
}

// A build with no console in it says so, in a sentence, rather than answering 404.
//
// The console is assembled by CI and this binary is buildable without it, which is a deliberate
// trade — see cmd/magi-web/console/README.md. The failure mode it buys is a reader who opens the
// address and gets nothing, so what they get instead has to name the missing step: a 404 reads as
// "wrong URL" and would send them looking at their own typing.
func TestABuildWithNoConsoleSaysSo(t *testing.T) {
	f := newFleetFixture(t)
	w := get(t, f.srv.page, "/")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a console-less build answered %d, which reads as a working page", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "console") {
		t.Errorf("the page does not say what is missing:\n%s", body)
	}
	// And it is not cached: the next build has one, and a browser holding this answer would go on
	// showing the apology to somebody standing in front of a working console.
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("the apology is cacheable (%q) — it would outlive the build it describes", cc)
	}
}

// A directory given with -console is read on every request.
//
// That flag is the development loop: gradle rebuilds the console into web/ui/build/console while
// the server runs, and a page cached at startup would show the reader the compile they had before
// the one they are waiting on. The embedded console cannot change under a running process, so only
// this path re-reads.
func TestAConsoleDirectoryIsRereadEachTime(t *testing.T) {
	f := newFleetFixture(t)
	withConsole(t, f.srv, map[string]string{"console.html": "<html>first</html>"})
	if got := get(t, f.srv.page, "/").Body.String(); !strings.Contains(got, "first") {
		t.Fatalf("the page did not come from the directory: %q", got)
	}
	if err := os.WriteFile(filepath.Join(f.srv.consoleDir, "console.html"),
		[]byte("<html>second</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := get(t, f.srv.page, "/").Body.String(); !strings.Contains(got, "second") {
		t.Errorf("a rebuilt console was not picked up: %q", got)
	}
}

// The cache contract under /ui/ is GWT's, and the two halves of it are opposite.
//
// A permutation is named by the hash of its own content — <hash>.cache.js — so it can be frozen
// forever and never be wrong. The selector that decides WHICH permutation a browser gets keeps one
// name across every build (<module>.nocache.js), so a browser holding yesterday's copy asks for a
// permutation that this build does not contain: the module never loads and the screen is blank,
// intermittently, for whoever happened to have visited before. With no headers at all a browser
// applies its own heuristic and caches both, which gets the immutable half right by luck and the
// other half wrong.
func TestTheCacheContractUnderUIIsGWTs(t *testing.T) {
	f := newFleetFixture(t)
	withConsole(t, f.srv, map[string]string{
		"console.html":           "<html></html>",
		"shell/shell.nocache.js": "var pick = 1;",
		"shell/A1B2C3.cache.js":  "var permutation = 1;",
		"console.css":            ".row {}",
	})
	frozen := get(t, f.srv.uiAsset, "/ui/shell/A1B2C3.cache.js")
	if frozen.Code != 200 {
		t.Fatalf("the permutation answered %d", frozen.Code)
	}
	if cc := frozen.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("a content-named permutation is refetched every time: %q", cc)
	}
	for _, name := range []string{"/ui/shell/shell.nocache.js", "/ui/console.css"} {
		w := get(t, f.srv.uiAsset, name)
		if w.Code != 200 {
			t.Fatalf("%s answered %d", name, w.Code)
		}
		cc := w.Header().Get("Cache-Control")
		if strings.Contains(cc, "max-age=") || strings.Contains(cc, "immutable") {
			t.Errorf("%s is frozen in the browser (%q) — it names a permutation that the next "+
				"build replaces", name, cc)
		}
		tag := w.Header().Get("ETag")
		if tag == "" {
			t.Fatalf("%s has no ETag, so revalidating it costs the whole file", name)
		}
		r := httptest.NewRequest(http.MethodGet, name, nil)
		r.Header.Set("If-None-Match", tag)
		again := httptest.NewRecorder()
		f.srv.uiAsset(again, r)
		if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Errorf("an unchanged %s answered %d with %d bytes", name, again.Code, again.Body.Len())
		}
	}
	// And nothing outside the tree is reachable through it.
	if w := get(t, f.srv.uiAsset, "/ui/../main.go"); w.Code != http.StatusNotFound {
		t.Errorf("a path with .. in it answered %d", w.Code)
	}
}
