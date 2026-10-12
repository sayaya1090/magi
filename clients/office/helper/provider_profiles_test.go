package office

import (
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
	if strings.Contains(get.Body.String(), "top-secret") || !strings.Contains(get.Body.String(), "next-start") {
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
