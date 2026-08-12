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

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
)

// A typed filename is a name, not a pattern.
//
// The tool takes a glob and refuses a malformed one, so "page[1]" — a perfectly ordinary filename —
// came back as a syntax error from a box that says "find in this workspace". What the caller wraps
// around the query stays a wildcard; what they typed means itself.
func TestATypedQueryIsNotAGlobPattern(t *testing.T) {
	for _, q := range []string{"page[1]", "a*b", "what?", `back\slash`} {
		quoted := globQuote(q)
		for _, meta := range []string{"[", "]", "*", "?"} {
			if i := strings.Index(quoted, meta); i > 0 && quoted[i-1] != '\\' {
				t.Errorf("%q became %q — the %s is still a wildcard", q, quoted, meta)
			}
		}
	}
	// And the ones the caller adds are untouched, or a name search would match only exact names.
	if got := "**/*" + globQuote("page") + "*"; got != "**/*page*" {
		t.Errorf("a plain query became %q", got)
	}
}

// The workspace routes go through the companion, and the companion's refusals come back whole.
//
// End to end over a real socket with a real daemon behind it, because the thing worth checking is
// the JOIN: that the console asks for a tool by name, that the tool's own answer arrives unchanged,
// and that a tool this door may not run is refused by the far side rather than by a check here
// that a second caller could walk around.
func TestTheWorkspaceIsReadThroughTheCompanion(t *testing.T) {
	f := newFleetFixture(t)
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "note.txt"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(wd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A daemon with the real read-only door on it: app.ReadOnlyTool, the allowlist, the tools.
	eng := &workspaceEngine{recordingEngine: &recordingEngine{},
		app: app.New(nil, nil, builtin.Default(), bus.New(), nil, app.Config{}), wd: wd}
	sock := f.liveDaemon(t, wd, "s1", eng)
	q := "?d=" + url.QueryEscape(sock)

	// The directory, as the tool answers it.
	w := httptest.NewRecorder()
	f.srv.files(w, httptest.NewRequest(http.MethodGet, "/files"+q, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("listing answered %d: %s", w.Code, w.Body.String())
	}
	var entries []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	if len(entries) != 2 || !entries[0].IsDir || entries[0].Name != "sub" {
		t.Errorf("the listing is %+v", entries)
	}

	// The file, line-numbered as the agent sees it.
	w = httptest.NewRecorder()
	f.srv.file(w, httptest.NewRequest(http.MethodGet, "/file"+q+"&path=note.txt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("reading answered %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "second") {
		t.Errorf("the file came back as %s", w.Body.String())
	}

	// And a path outside the workspace is the companion's refusal, in its own words.
	w = httptest.NewRecorder()
	f.srv.file(w, httptest.NewRequest(http.MethodGet, "/file"+q+"&path=../../etc/passwd", nil))
	if w.Code == http.StatusOK {
		t.Errorf("a path outside the workspace was read: %s", w.Body.String())
	}

	// A search finds the line, in the shape the agent's own grep produces.
	w = httptest.NewRecorder()
	f.srv.find(w, httptest.NewRequest(http.MethodGet, "/find"+q+"&in=text&q=second", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("searching answered %d: %s", w.Code, w.Body.String())
	}
	var found findAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &found); err != nil {
		t.Fatal(err)
	}
	if len(found.Hits) != 1 || !strings.HasPrefix(found.Hits[0], "note.txt:2:") {
		t.Errorf("the search found %v", found.Hits)
	}
}

// workspaceEngine is a daemon that can read its own workspace, which is what the real one is.
type workspaceEngine struct {
	*recordingEngine
	app *app.App
	wd  string
}

func (e *workspaceEngine) ReadOnlyTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return e.app.ReadOnlyTool(ctx, e.wd, name, args)
}

// A name search reaches the whole tree, not the root of it.
//
// Somebody typing three letters of a filename means the file wherever it is; a pattern without **
// matches only what is directly in the workspace, and the answer — an empty list — reads as "there
// is no such file" rather than as "this only looked in one directory".
func TestANameSearchReachesTheWholeTree(t *testing.T) {
	f := newFleetFixture(t)
	wd := t.TempDir()
	deep := filepath.Join(wd, "cmd", "magi")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := &workspaceEngine{recordingEngine: &recordingEngine{},
		app: app.New(nil, nil, builtin.Default(), bus.New(), nil, app.Config{}), wd: wd}
	sock := f.liveDaemon(t, wd, "s1", eng)

	w := httptest.NewRecorder()
	f.srv.find(w, httptest.NewRequest(http.MethodGet,
		"/find?d="+url.QueryEscape(sock)+"&q=main", nil))
	var got findAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Hits) != 1 || got.Hits[0] != "cmd/magi/main.go" {
		t.Errorf("a name search two directories down found %v", got.Hits)
	}

	// And a filename with a bracket in it is a filename. The tool refuses a malformed glob, so
	// without quoting, "page[1" comes back from a box labelled "find in this workspace" as a
	// syntax error about a character class nobody wrote.
	if err := os.WriteFile(filepath.Join(wd, "page[1].txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	f.srv.find(w, httptest.NewRequest(http.MethodGet,
		"/find?d="+url.QueryEscape(sock)+"&q="+url.QueryEscape("page[1]"), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("searching for a bracketed filename answered %d: %s", w.Code, w.Body.String())
	}
	got = findAnswer{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Hits) != 1 || got.Hits[0] != "page[1].txt" {
		t.Errorf("a filename with a bracket in it found %v", got.Hits)
	}
}
