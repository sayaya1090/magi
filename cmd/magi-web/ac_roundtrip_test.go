package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	fn := js[i : i+2000]
	if !strings.Contains(fn, `setAttribute('selected', '')`) {
		t.Error("the chosen option is marked only as a property, which the component's reset drops")
	}
	if !strings.Contains(fn, "updateComplete") {
		t.Error("value is assigned without waiting for the component, so the lookup finds no options")
	}
}
