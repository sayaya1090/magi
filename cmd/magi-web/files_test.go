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
	"sync"
	"testing"
	"time"

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

// A call that runs the model does not stop the console asking anything else about that companion.
//
// The pooled client holds one mutex across a whole round trip, so a drafted commit message — tens
// of seconds of a model — was tens of seconds in which the tree, the git card and the queue could
// not be read. Measured on the live console before this: with a draft in flight, a request for the
// file tree took 2.7 seconds against 0.6 milliseconds idle. It was not slow; it was queued behind
// a model.
func TestAModelCallDoesNotBlockTheRestOfTheConsole(t *testing.T) {
	f := newFleetFixture(t)
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "note.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	eng := &slowModel{workspaceEngine: &workspaceEngine{recordingEngine: &recordingEngine{},
		app: app.New(nil, nil, builtin.Default(), bus.New(), nil, app.Config{}), wd: wd},
		held: held, started: make(chan struct{})}
	sock := f.liveDaemon(t, wd, "s1", eng)
	q := "?d=" + url.QueryEscape(sock)

	// The slow one, still going.
	drafted := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		f.srv.gitMsg(w, httptest.NewRequest(http.MethodPost, "/git-msg"+q, nil))
		drafted <- w.Code
	}()
	// It has to be IN the call before the second request is made, or this proves nothing.
	select {
	case <-eng.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the draft never reached the daemon")
	}

	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		f.srv.files(w, httptest.NewRequest(http.MethodGet, "/files"+q, nil))
		done <- w.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("the listing answered %d while a model call was in flight", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the file tree could not be read while the companion was drafting a message")
	}
	close(held)
	if code := <-drafted; code != http.StatusOK {
		t.Errorf("the draft answered %d", code)
	}
}

// slowModel is a companion whose model takes as long as this test wants it to.
type slowModel struct {
	*workspaceEngine
	held    chan struct{}
	begin   sync.Once
	started chan struct{}
}

func (e *slowModel) DraftCommit(ctx context.Context) (string, error) {
	e.begin.Do(func() { close(e.started) })
	<-e.held
	return "a message", nil
}

// The rest of Reviewer, because the daemon routes by the whole interface: an engine missing one of
// these is one that answers "this daemon cannot review" and never reaches the method under test.
func (e *slowModel) LookOver(ctx context.Context, path, text string) (string, error) {
	return "", nil
}
func (e *slowModel) OpenPR(ctx context.Context, title, body string) (string, error) {
	return "", nil
}
func (e *slowModel) PRFacts(ctx context.Context) (string, error) { return "{}", nil }
func (e *slowModel) DraftPR(ctx context.Context) (string, error) { return "", nil }
func (e *slowModel) CompleteCode(ctx context.Context, path, prefix, suffix string) (string, error) {
	return "", nil
}
func (e *slowModel) SetOpenFile(ctx context.Context, path, text string) error { return nil }
func (e *slowModel) SuggestPrompt(ctx context.Context, prefix string) (string, error) {
	return "", nil
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
