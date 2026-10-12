package appbridge

import (
	"encoding/json"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsRequireAuthenticationAndBoundWorkspace(t *testing.T) {
	s, e := New(Config{App: "figma", Token: "token", ConfigDir: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	socket := mockDaemon(t, func(map[string]any) any { return map[string]any{"ok": true} })
	s.bindings["a"] = &Binding{ID: "a", Target: "file-a", Session: "sa", socket: socket}
	s.bindings["b"] = &Binding{ID: "b", Target: "file-b", Session: "sb"}
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			r.Header.Set("Authorization", "Bearer token")
		}
		w := httptest.NewRecorder()
		s.Mux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/instructions", "/api/guides", "/api/council", "/api/provider-profiles"} {
		if w := call("GET", path, "", false); w.Code != 401 {
			t.Fatalf("unauthenticated %s", path)
		}
	}
	if w := call("POST", "/api/instructions", `{"binding":"a","text":"always preserve layers"}`, true); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := call("GET", "/api/instructions?binding=b", "", true); strings.Contains(w.Body.String(), "preserve layers") {
		t.Fatal("rules leaked across documents")
	}
	if w := call("POST", "/api/instructions", `{"binding":"missing","text":"bad"}`, true); w.Code == 200 {
		t.Fatal("unbound write accepted")
	}
	if w := call("POST", "/api/guide", `{"binding":"a","name":"design","op":"save","body":"Design rules\nbody"}`, true); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := call("POST", "/api/guide", `{"binding":"a","name":"design","op":"disable"}`, true); w.Code != 200 {
		t.Fatal(w.Body)
	}
	w := call("GET", "/api/guides?binding=a", "", true)
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Body)
	}
	w = call("GET", "/api/guides?binding=b", "", true)
	if strings.Contains(w.Body.String(), "design") {
		t.Fatal("guide leaked")
	}
	if w := call("POST", "/api/guide", `{"binding":"a","name":"../escape","op":"save","body":"bad"}`, true); w.Code == 200 {
		t.Fatal("traversal accepted")
	}
	if w := call("POST", "/api/council", `{"binding":"a","enabled":false}`, true); w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = call("GET", "/api/council?binding=b", "", true)
	if !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("council leaked across targets")
	}
	if w := call("POST", "/api/provider-profiles", `{"op":"save","name":"fast","baseUrl":"https://example.com/v1","model":"small","apiKey":"hidden-key"}`, true); w.Code != 200 || strings.Contains(w.Body.String(), "hidden-key") {
		t.Fatal("save failed or secret leaked")
	}
	if w := call("POST", "/api/provider-profiles", `{"op":"save","name":"fast","baseUrl":"https://example.com/v1","model":"new"}`, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"hasKey":true`) {
		t.Fatal("key not preserved")
	}
	w = call("GET", "/api/provider-profiles", "", true)
	var out struct {
		Profiles []providerProfile `json:"profiles"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Profiles) != 1 || !out.Profiles[0].HasKey {
		t.Fatal("list contract")
	}
	wd, _ := s.settingsWorkspace("a")
	if err := os.Remove(filepath.Join(wd, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("unchanged"), 0600)
	if err := os.Symlink(outside, filepath.Join(wd, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if w := call("POST", "/api/instructions", `{"binding":"a","text":"bad"}`, true); w.Code == 200 {
		t.Fatal("symlink accepted")
	}
	body, _ := os.ReadFile(outside)
	if string(body) != "unchanged" {
		t.Fatal("outside file changed")
	}
}

func TestSettingsGuideDescriptionMatchesFrontmatter(t *testing.T) {
	if got := settingsGuideDescription("---\ndescription: \"Layer preservation\"\n---\nBody"); got != "Layer preservation" {
		t.Fatal("frontmatter description mismatch")
	}
}

func TestSettingsCouncilLayerInheritanceAndSkillsReload(t *testing.T) {
	dir := t.TempDir()
	if err := config.SetRawKey(filepath.Join(dir, "config.toml"), "council", "enabled", "false"); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{App: "figma", Token: "token", ConfigDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	socket := mockDaemon(t, func(map[string]any) any { return map[string]any{"ok": true} })
	s.bindings["a"] = &Binding{ID: "a", Target: "file-a", Session: "sa", socket: socket}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer token")
		w := httptest.NewRecorder()
		s.Mux.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/council?binding=a", ""); !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("global council false was not inherited")
	}
	wd, _ := s.settingsWorkspace("a")
	if err := config.SetRawKey(filepath.Join(wd, ".magi", "config.toml"), "council", "enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/council?binding=a", ""); !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("workspace override was not inherited")
	}
	if w := call("POST", "/api/council", `{"binding":"a","enabled":false}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("companion override did not win")
	}
	loader := &app.App{}
	if len(loader.Skills(wd)) != 0 {
		t.Fatal("unexpected skills")
	}
	for _, step := range []struct {
		body  string
		count int
		text  string
	}{
		{`{"binding":"a","name":"live","op":"save","body":"First body"}`, 1, "First body"},
		{`{"binding":"a","name":"live","op":"save","body":"Second changed body"}`, 1, "Second changed body"},
		{`{"binding":"a","name":"live","op":"disable"}`, 0, ""},
		{`{"binding":"a","name":"live","op":"enable"}`, 1, "Second changed body"},
	} {
		if w := call("POST", "/api/guide", step.body); w.Code != 200 || !strings.Contains(w.Body.String(), "다음 요청") {
			t.Fatal("guide update failed")
		}
		skills := loader.Skills(wd)
		if len(skills) != step.count {
			t.Fatalf("existing loader cache did not invalidate: got %d want %d", len(skills), step.count)
		}
		if len(skills) > 0 && skills[0].Body != step.text {
			t.Fatal("loader retained stale skill body")
		}
	}
}
