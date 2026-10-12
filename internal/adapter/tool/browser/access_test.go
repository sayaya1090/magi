package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func TestProxyUsesCallerSessionAndPreservesResult(t *testing.T) {
	dir := t.TempDir()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer 0123456789abcdef" {
			t.Error("missing authentication")
		}
		var p map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&p)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/browser/available":
			w.Write([]byte(`{"bindings":[{"binding":"primary"}]}`))
		case "/api/browser/attach":
			if string(p["session"]) != `"s1"` || string(p["socket"]) != `"fixed.sock"` || string(p["proxy"]) != "true" {
				t.Error(p)
			}
			json.NewEncoder(w).Encode(map[string]any{"lease": "lease1", "tools": []any{map[string]any{"name": "read_page"}}, "expiresAt": time.Now().Add(time.Minute).UTC()})
		case "/api/browser/call":
			calls++
			if string(p["session"]) != `"s1"` {
				t.Error("session changed")
			}
			w.Write([]byte(`{"result":{"text":"original\nunchanged","empty":""}}`))
		case "/api/browser/release":
			w.Write([]byte(`{"released":true}`))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer srv.Close()
	cleanup, err := Publish(dir, "headless", Runtime{URL: srv.URL, Token: "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a := &Access{ConfigDir: dir, Socket: func(port.ToolEnv) string { return "fixed.sock" }}
	env := port.ToolEnv{SessionID: "s1"}
	connected, _ := a.Execute(context.Background(), json.RawMessage(`{"action":"connect"}`), env)
	if connected.IsError || strings.Contains(string(connected.Content), "0123456789abcdef") || strings.Contains(string(connected.Content), "lease1") {
		t.Fatal(string(connected.Content))
	}
	foreign, _ := a.Execute(context.Background(), json.RawMessage(`{"action":"call","tool":"read_page","args":{}}`), port.ToolEnv{SessionID: "s2"})
	if !foreign.IsError || calls != 0 {
		t.Fatal("foreign session borrowed lease")
	}
	result, _ := a.Execute(context.Background(), json.RawMessage(`{"action":"call","tool":"read_page","args":{}}`), env)
	if result.IsError || string(result.Content) != `{"text":"original\nunchanged","empty":""}` || calls != 1 {
		t.Fatal(string(result.Content))
	}
	released, _ := a.Execute(context.Background(), json.RawMessage(`{"action":"release"}`), env)
	if released.IsError {
		t.Fatal(string(released.Content))
	}
	if len(a.states) != 0 {
		t.Fatal("released session state retained")
	}
	after, _ := a.Execute(context.Background(), json.RawMessage(`{"action":"call","tool":"read_page","args":{}}`), env)
	if !after.IsError || calls != 1 {
		t.Fatal("released lease executed")
	}
}

func TestRuntimeRejectsRemoteEndpointsAndUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	for _, u := range []string{"https://example.com", "http://localhost:8737", "http://127.0.0.1:8737/other", "http://127.0.0.1:8737/?token=secret"} {
		if _, err := Publish(dir, "headless", Runtime{URL: u, Token: "0123456789abcdef"}); err == nil {
			t.Fatal(u)
		}
	}
	cleanup, err := Publish(dir, "headless", Runtime{URL: "http://127.0.0.1:8739", Token: "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	path := filepath.Join(dir, "runtime", "browser-headless.json")
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = readRuntime(dir, "headless"); err == nil {
		t.Fatal("public credentials accepted")
	}
}

func TestOldCleanupDoesNotDeleteReplacement(t *testing.T) {
	dir := t.TempDir()
	old, err := Publish(dir, "extension", Runtime{URL: "http://127.0.0.1:8737", Token: "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := Publish(dir, "extension", Runtime{URL: "http://127.0.0.1:8737", Token: "replacement-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer current()
	old()
	if _, err = readRuntime(dir, "extension"); err != nil {
		t.Fatal("old cleanup removed replacement")
	}
}

func TestExpiredAccessStateIsSwept(t *testing.T) {
	a := &Access{ConfigDir: t.TempDir(), states: map[string]*accessState{}}
	for i := 0; i < 100; i++ {
		a.states[fmt.Sprint(i)] = &accessState{connected: true, lease: accessLease{expires: time.Now().Add(-time.Minute)}}
	}
	result, _ := a.Execute(context.Background(), json.RawMessage(`{"action":"call","tool":"read_page","args":{}}`), port.ToolEnv{SessionID: "fresh"})
	if !result.IsError || len(a.states) != 0 {
		t.Fatal("expired state accumulated", len(a.states))
	}
}
