package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *fleetFixture) profiles(t *testing.T) []llmProfile {
	t.Helper()
	w := httptest.NewRecorder()
	f.srv.profilesList(w, httptest.NewRequest(http.MethodGet, "/profiles", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/profiles answered %d: %s", w.Code, w.Body.String())
	}
	var out []llmProfile
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable: %v", err)
	}
	return out
}

// Editing a profile's endpoint or model must not wipe a key set earlier, the GET must never carry
// the key value, and clearKey removes it. The whole point of the write-only key handling.
func TestAProfileKeyIsWriteOnlyAndSurvivesAnEdit(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	writeConfig(t, filepath.Join(wd, ".magi"), "model = \"qwen3\"\n")
	q := "/profiles?d=" + url.QueryEscape(sock)
	cfgPath := filepath.Join(wd, ".magi", "config.toml")

	// Add one, with a key.
	if w := post(t, f.srv, f.srv.profilesList, q, url.Values{
		"name": {"fast"}, "baseUrl": {"http://localhost:11434/v1"},
		"model": {"gpt-oss:20b"}, "apiKey": {"super-secret"}}); w.Code != http.StatusOK {
		t.Fatalf("adding answered %d: %s", w.Code, w.Body.String())
	}
	find := func(name string) (llmProfile, bool) {
		for _, p := range f.profiles(t) {
			if p.Name == name {
				return p, true
			}
		}
		return llmProfile{}, false
	}
	got, ok := find("fast")
	if !ok || got.BaseURL != "http://localhost:11434/v1" || got.Model != "gpt-oss:20b" {
		t.Fatalf("after adding: %+v (ok=%v)", got, ok)
	}
	if !got.HasKey {
		t.Errorf("the profile does not report a key: %+v", got)
	}
	// The value is never serialised — HasKey bool is all the browser gets.
	if b, _ := json.Marshal(got); strings.Contains(string(b), "super-secret") {
		t.Errorf("the key value went over the wire: %s", b)
	}
	// It IS in the file (that is where a secret lives), just not on the wire.
	if body, _ := os.ReadFile(cfgPath); !strings.Contains(string(body), "super-secret") {
		t.Fatalf("the key was not written to the file:\n%s", body)
	}

	// Change only the model — no apiKey field — and the key must stay.
	if w := post(t, f.srv, f.srv.profilesList, q, url.Values{
		"name": {"fast"}, "baseUrl": {"http://localhost:11434/v1"}, "model": {"llama3"}}); w.Code != http.StatusOK {
		t.Fatalf("changing answered %d: %s", w.Code, w.Body.String())
	}
	if body, _ := os.ReadFile(cfgPath); !strings.Contains(string(body), "super-secret") {
		t.Errorf("editing the model wiped the key:\n%s", body)
	}
	if got, _ := find("fast"); !got.HasKey || got.Model != "llama3" {
		t.Errorf("after the edit: %+v", got)
	}

	// clearKey removes it.
	if w := post(t, f.srv, f.srv.profilesList, q, url.Values{
		"name": {"fast"}, "baseUrl": {"http://localhost:11434/v1"}, "model": {"llama3"},
		"clearKey": {"1"}}); w.Code != http.StatusOK {
		t.Fatalf("clearing the key answered %d: %s", w.Code, w.Body.String())
	}
	if body, _ := os.ReadFile(cfgPath); strings.Contains(string(body), "api_key") {
		t.Errorf("clearKey left the key in the file:\n%s", body)
	}
	if got, _ := find("fast"); got.HasKey {
		t.Errorf("the profile still reports a key after clearKey: %+v", got)
	}
	// The unrelated key is untouched throughout.
	if body, _ := os.ReadFile(cfgPath); !strings.Contains(string(body), `model = "qwen3"`) {
		t.Errorf("the rest of the file was lost:\n%s", body)
	}
}

// The name becomes a TOML table header, so anything that is not a bare key is refused — including a
// newline or other control character, which would otherwise split the header and leave the whole
// config.toml unparseable (the daemon's next start fails). None of these is ever written.
func TestAProfileNameThatWouldBreakTheFileIsRefused(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "api", true)
	f.session("api", wd, "x", 1, false)
	q := "/profiles?d=" + url.QueryEscape(sock)

	for _, tc := range []struct {
		why  string
		name string
	}{
		{"a space", "my profile"},
		{"a dot", "a.b"},
		{"a bracket", "a]b"},
		{"a quote", `a"b`},
		{"a hash", "a#b"},
		{"a newline", "foo\nbar"},
		{"a carriage return", "foo\rbar"},
		{"a NUL", "foo\x00bar"},
		{"a comma", "a,b"},
		{"a colon", "a:b"},
		{"an equals", "a=b"},
		{"a slash", "a/b"},
		{"a non-ASCII letter", "café"},
		{"no name at all", ""},
	} {
		w := post(t, f.srv, f.srv.profilesList, q, url.Values{
			"name": {tc.name}, "baseUrl": {"http://localhost:11434/v1"}})
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400 (%s)", tc.why, w.Code, w.Body.String())
		}
	}
	if list := f.profiles(t); len(list) != 0 {
		t.Errorf("a refused name was written anyway: %+v", list)
	}
	// The allowlist must not over-reject: a bare-key name (letters, digits, hyphen, underscore) is
	// exactly what a model profile is usually called, and it is accepted.
	if w := post(t, f.srv, f.srv.profilesList, q, url.Values{
		"name": {"code-fast_2"}, "baseUrl": {"http://localhost:11434/v1"}}); w.Code != http.StatusOK {
		t.Errorf("a valid bare-key name was refused: %d (%s)", w.Code, w.Body.String())
	}
}
