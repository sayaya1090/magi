package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// Choosing a completion profile is a setting that has to survive the dialog being closed. It is
// written to a config file and read back from one, and those are two different code paths that can
// disagree about WHICH file — the global one or the companion's — without anything failing.
func TestAChosenProfileIsStillChosenWhenTheDialogIsReopened(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{cfgDir: dir}

	post := httptest.NewRequest(http.MethodPost, "/autocomplete",
		strings.NewReader("codeProfile=embedding&tier=global"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wp := httptest.NewRecorder()
	s.autocomplete(wp, post)
	if wp.Code != http.StatusOK {
		t.Fatalf("the save was refused: %d %s", wp.Code, wp.Body.String())
	}

	// What the file actually holds, before trusting the reader that is supposed to find it.
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `code_profile = "embedding"`) {
		t.Fatalf("the choice never reached the file:\n%s", raw)
	}

	wg := httptest.NewRecorder()
	s.autocomplete(wg, httptest.NewRequest(http.MethodGet, "/autocomplete?tier=global", nil))
	if wg.Code != http.StatusOK {
		t.Fatalf("the read failed: %d %s", wg.Code, wg.Body.String())
	}
	if !strings.Contains(wg.Body.String(), `"codeProfile":"embedding"`) {
		t.Errorf("the dialog reopens with the choice gone: %s", wg.Body.String())
	}
}

// The other half of the same bug, and the half that actually showed: the server returned the saved
// profile and the picker still opened blank.
//
// md-select's value setter is `select(v)`, which looks v up in `this.menu?.items ?? []` and gives
// up quietly when there is no match. Assigned in the same tick the options were appended, the menu
// has not rendered yet, so it matches nothing — every time, silently. And the option's selection
// was carried only as a property, while the component's own reset() reads the ATTRIBUTE.
func TestThePickerMarksTheSavedProfileWithoutRacingTheComponent(t *testing.T) {
	b, err := pageFS.ReadFile("page.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	i := strings.Index(js, "const fillProfiles")
	if i < 0 {
		t.Fatal("the profile picker no longer fills itself here")
	}
	// To the end of the function, not a fixed number of characters. It was js[i:i+2000], and the
	// window stopped covering the tail the moment the function grew — a test that silently narrows
	// as the code it guards gets longer.
	end := strings.Index(js[i:], "\n};")
	if end < 0 {
		t.Fatal("fillProfiles does not end where this expects")
	}
	fn := js[i : i+end]
	if !strings.Contains(fn, `setAttribute('selected', '')`) {
		t.Error("the chosen option is marked only as a property, which the component's reset drops")
	}
	if !strings.Contains(fn, "updateComplete") {
		t.Error("value is assigned without waiting for the component, so the lookup finds no options")
	}
}

// The age is counted from the prompt that opened the turn — the same instant the terminal counts
// from — so the two surfaces cannot disagree about how long a companion has been busy.
func TestTheTurnsAgeIsCountedFromThePromptThatOpenedIt(t *testing.T) {
	now := time.Now()
	msgs := []session.Message{
		{Role: session.RoleUser, At: now.Add(-9 * time.Minute)},
		{Role: session.RoleAssistant, At: now.Add(-5 * time.Minute)},
		{Role: session.RoleUser, At: now.Add(-90 * time.Second)},
		{Role: session.RoleAssistant, At: now.Add(-30 * time.Second)},
	}
	if got := turnAgeSec(msgs, true); got < 88 || got > 92 {
		t.Errorf("the age is %ds; it should be counted from the LAST prompt, not the first", got)
	}
	// Nothing to say when nothing is running.
	if got := turnAgeSec(msgs, false); got != 0 {
		t.Errorf("a closed turn reported an age of %d", got)
	}
	// A message assembled rather than replayed carries a zero time, and counting from 1970 would
	// put half a century on the screen.
	if got := turnAgeSec([]session.Message{{Role: session.RoleUser}}, true); got != 0 {
		t.Errorf("a prompt with no recorded time produced an age of %d", got)
	}
}
