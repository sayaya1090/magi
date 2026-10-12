package office

import (
	"encoding/json"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
)

func TestProviderProfilesPersistenceAndSecretBoundaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("# operator comment\nmodel = \"original\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &API{ConfigDir: dir}
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		a.providerProfiles(w, httptest.NewRequest("POST", "/api/provider-profiles", strings.NewReader(body)))
		return w
	}
	w := post(`{"op":"save","name":"fast","baseUrl":"https://example.com/v1","model":"small","apiKey":"top-secret"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "top-secret") || !strings.Contains(w.Body.String(), `"hasKey":true`) {
		t.Fatalf("response %d %s", w.Code, w.Body)
	}
	w = post(`{"op":"save","name":"fast","baseUrl":"http://localhost:9999/v1","model":"new","apiKey":""}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.LLM.Profiles["fast"]; p.APIKey != "top-secret" || p.Model != "new" {
		t.Fatal("profile values did not persist")
	}
	get := httptest.NewRecorder()
	a.providerProfiles(get, httptest.NewRequest("GET", "/api/provider-profiles", nil))
	if strings.Contains(get.Body.String(), "top-secret") || !strings.Contains(get.Body.String(), "session-apply") {
		t.Fatal(get.Body)
	}
	before, _ := os.ReadFile(path)
	for _, body := range []string{
		`{"op":"save","name":"bad.name","baseUrl":"https://example.com","model":"m"}`,
		`{"op":"save","name":"fast","baseUrl":"file:///tmp/key","model":"m"}`,
		`{"op":"save","name":"fast","baseUrl":"https://example.com?key=hidden","model":"m"}`,
		`{"op":"save","name":"fast","baseUrl":"https://secret@example.com","model":"m"}`,
		`{"op":"save","name":"fast","baseUrl":"https://example.com","model":""}`,
	} {
		if w := post(body); w.Code != 400 {
			t.Fatalf("accepted invalid %s", body)
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("rejected input changed config")
	}
	w = post(`{"op":"save","name":"fast","baseUrl":"https://example.com","model":"new","clearKey":true}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"hasKey":false`) {
		t.Fatal(w.Body)
	}
	w = post(`{"op":"delete","name":"fast"}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	cfg, err = config.Load(dir)
	if err != nil || len(cfg.LLM.Profiles) != 0 {
		t.Fatal("profile deletion failed")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "# operator comment") || cfg.Model != "original" {
		t.Fatal("unrelated config changed")
	}
}

func TestProviderProfileRouteRequiresAuthentication(t *testing.T) {
	a := &API{ConfigDir: t.TempDir(), Token: "token"}
	mux := http.NewServeMux()
	a.Route(mux)
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, "/api/provider-profiles", strings.NewReader(`{}`)))
		if w.Code == 200 {
			t.Fatal("unauthenticated route accepted")
		}
	}
}

func TestProviderProfileApplyNeedsBoundConversation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"original\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	a := &API{ConfigDir: dir}
	w := httptest.NewRecorder()
	a.providerProfiles(w, httptest.NewRequest("POST", "/api/provider-profiles", strings.NewReader(`{"op":"apply","name":"saved"}`)))
	if w.Code != http.StatusConflict {
		t.Fatalf("unbound apply returned %d", w.Code)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("apply mutated configuration")
	}
}

func TestSavedProfilesAppearInPickerBeforeConversationBinding(t *testing.T) {
	dir := t.TempDir()
	if err := config.SetKeysChecked(filepath.Join(dir, "config.toml"), "llm.profiles.friendly", map[string]string{"base_url": "https://example.com/v1", "api_key": "hidden-key", "model": "model-a"}); err != nil {
		t.Fatal(err)
	}
	a := &API{ConfigDir: dir}
	w := httptest.NewRecorder()
	a.models(w, httptest.NewRequest("GET", "/api/models", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"friendly"`) || strings.Contains(w.Body.String(), "hidden-key") {
		t.Fatalf("picker response: %d %s", w.Code, w.Body)
	}
}

func TestSavingSelectedProviderRefreshesCurrentSession(t *testing.T) {
	for _, tc := range []struct {
		name, selected, applyError string
		wantApply                  bool
	}{{"selected", "friendly", "", true}, {"other", "other", "", false}, {"busy", "friendly", "busy", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sockDir, err := os.MkdirTemp("", "mpp-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(sockDir)
			socket := filepath.Join(sockDir, "d.sock")
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			requests := make(chan daemon.Request, 4)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					c, e := ln.Accept()
					if e != nil {
						return
					}
					var r daemon.Request
					e = json.NewDecoder(c).Decode(&r)
					if e == nil {
						requests <- r
						out := daemon.Response{OK: true, Profile: tc.selected}
						if r.Method == "use-profile" {
							out.Err = tc.applyError
							out.OK = tc.applyError == ""
						}
						if err := json.NewEncoder(c).Encode(out); err != nil {
							return
						}
					}
					if err := c.Close(); err != nil {
						return
					}
				}
			}()
			a := &API{ConfigDir: dir, Bridge: &Bridge{socket: socket, session: "current"}}
			w := httptest.NewRecorder()
			a.providerProfiles(w, httptest.NewRequest("POST", "/api/provider-profiles", strings.NewReader(`{"op":"save","name":"friendly","baseUrl":"https://example.com/v1","model":"model-b","apiKey":"secret-key"}`)))
			ln.Close()
			<-done
			if w.Code != 200 || strings.Contains(w.Body.String(), "secret-key") {
				t.Fatalf("save: %d %s", w.Code, w.Body)
			}
			gotApply := false
			for len(requests) > 0 {
				r := <-requests
				if r.Method == "use-profile" {
					gotApply = true
					if r.Name != "friendly" || r.Session != "current" {
						t.Fatal("wrong profile/session")
					}
				}
			}
			if gotApply != tc.wantApply {
				t.Fatalf("apply=%v want=%v", gotApply, tc.wantApply)
			}
			if tc.applyError != "" && !strings.Contains(w.Body.String(), "반영하지 못했습니다") {
				t.Fatal("application failure hidden")
			}
			if tc.applyError == "" && tc.wantApply && !strings.Contains(w.Body.String(), "현재 대화에 반영했습니다") {
				t.Fatal("missing application confirmation")
			}
		})
	}
}
