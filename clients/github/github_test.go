package githubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTargets(t *testing.T) {
	for _, s := range []string{"owner/repo", "owner/repo/issues/12", "owner/repo/pull/42"} {
		if _, e := ParseTarget(s); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"../repo", "owner/..", "https://evil/owner/repo", "owner/repo/issues/no", "owner/repo/pull/1/extra"} {
		if _, e := ParseTarget(s); e == nil {
			t.Fatal(s)
		}
	}
}
func TestBoundRead(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/o/r/issues/12" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" || r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("request %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"title": "Bound issue", "body": strings.Repeat("x", 12001), "secret": "not projected"})
	}))
	defer srv.Close()
	api := API{Token: "test-token", BaseURL: srv.URL}
	v, e := api.Call(context.Background(), "o/r/issues/12", "github_issue", json.RawMessage(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	m := v.(map[string]any)
	if m["truncated"] != true || m["secret"] != nil {
		t.Fatal(m)
	}
	for _, args := range []string{`{"repo":"other"}`, `[]`, `null`} {
		if _, e := api.Call(context.Background(), "o/r/issues/12", "github_issue", json.RawMessage(args)); e == nil {
			t.Fatal(args)
		}
	}
	if _, e := api.Call(context.Background(), "o/r", "github_issue", json.RawMessage(`{}`)); e == nil {
		t.Fatal("issue outside issue target")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestAPIStatusAndSize(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{403, "secret server response"}, {200, strings.Repeat("x", (1<<20)+1)}, {200, "not json"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
		_, e := (API{BaseURL: srv.URL}).Call(context.Background(), "o/r", "github_repository", json.RawMessage(`{}`))
		srv.Close()
		if e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatalf("unsafe error %v", e)
		}
	}
}
