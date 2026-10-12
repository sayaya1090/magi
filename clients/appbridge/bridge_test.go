package appbridge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func mockDaemon(t *testing.T, serve func(map[string]any) any) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "magi-bridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				scan := bufio.NewScanner(c)
				for scan.Scan() {
					var p map[string]any
					if json.Unmarshal(scan.Bytes(), &p) != nil {
						return
					}
					if e := json.NewEncoder(c).Encode(serve(p)); e != nil {
						return
					}
				}
			}()
		}
	}()
	return path
}
func TestConnectAttachAndRequestsKeepSessionAndTarget(t *testing.T) {
	var mu sync.Mutex
	count := 0
	requests := []map[string]any{}
	var server *Server
	path := mockDaemon(t, func(p map[string]any) any {
		mu.Lock()
		defer mu.Unlock()
		switch p["method"] {
		case "sessions":
			return map[string]any{"ok": true, "sessions": []any{}}
		case "session-new":
			count++
			if p["keep"] != true {
				t.Error("session creation switched foreground")
			}
			return map[string]any{"ok": true, "session": fmt.Sprintf("s_%d", count)}
		case "mcp-attach":
			// Initialization reaches the binding before attachment finishes: no global lock held.
			url, _ := p["url"].(string)
			if url == "" {
				url, _ = p["name"].(string)
			}
			_ = url
			server.mu.Lock()
			if len(server.bindings) == 0 {
				t.Error("binding unavailable during attachment")
			}
			server.mu.Unlock()
			return map[string]any{"ok": true, "tools": []string{"mcp__mail__mail_read"}}
		default:
			requests = append(requests, p)
			return map[string]any{"ok": true}
		}
	})
	var err error
	server, err = New(Config{App: "mail", Token: "test-token", Tools: []Tool{{Name: "mail_read"}}, Socket: func(context.Context, string) (string, error) { return path, nil }})
	if err != nil {
		t.Fatal(err)
	}
	a, err := server.Connect(context.Background(), "account/inbox/1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := server.Connect(context.Background(), "account/inbox/2")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := server.Connect(context.Background(), a.Target)
	if a.ID == b.ID || a.Session == b.Session || again.ID != a.ID {
		t.Fatal("target binding leaked")
	}
	_, err = server.Request(context.Background(), a.ID, Request{Method: "say", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	p := requests[len(requests)-1]
	mu.Unlock()
	if p["session"] != a.Session || p["method"] != "submit" {
		t.Fatalf("wrong request: %v", p)
	}
	if _, err = server.Request(context.Background(), a.ID, Request{Method: "shell", Text: "pwd"}); err == nil {
		t.Fatal("administrative command accepted")
	}
}
func TestAuthMalformedInputAndUnknownBinding(t *testing.T) {
	s, _ := New(Config{App: "figma", Token: "test-token"})
	for _, token := range []string{"", "bad"} {
		r := httptest.NewRequest("POST", "/api/connect", strings.NewReader(`{"target":"file"}`))
		r.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		s.Mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/request", strings.NewReader(`{"binding":"unknown","method":"say"}`))
	r.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.Mux.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{"target":"a"}{"target":"b"}`, `[1]`} {
		r = httptest.NewRequest("POST", "/api/connect", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer test-token")
		w = httptest.NewRecorder()
		s.Mux.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	if _, err := New(Config{App: "mail", Addr: "0.0.0.0:9000"}); err == nil {
		t.Fatal("public listener allowed")
	}
}
func TestRelayRejectsForeignBindingAndLateResult(t *testing.T) {
	s, _ := New(Config{App: "figma", Token: "test-token"})
	b := Binding{ID: "bound", Target: "file", pending: make(chan *toolCall, 1)}
	s.bindings[b.ID] = &b
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.relay(ctx, b, "figma_selection", json.RawMessage(`{}`)); done <- err }()
	var call *toolCall
	select {
	case call = <-b.pending:
	case <-time.After(time.Second):
		t.Fatal("no relay")
	}
	post := func(binding string) int {
		data, _ := json.Marshal(map[string]any{"binding": binding, "callId": call.ID, "result": "ok"})
		r := httptest.NewRequest("POST", "/api/figma/result", strings.NewReader(string(data)))
		r.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		s.Mux.ServeHTTP(w, r)
		return w.Code
	}
	if post("foreign") != http.StatusConflict {
		t.Fatal("foreign result accepted")
	}
	if post(b.ID) != 200 {
		t.Fatal("valid result refused")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if post(b.ID) != 409 {
		t.Fatal("duplicate result accepted")
	}
}

func TestSocketIdentityIsComputedAfterWorkspaceExists(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	binary := filepath.Join(root, "fake-magi")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{App: "browser", ConfigDir: alias, Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	sock, err := s.socket(context.Background(), "new-target")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("new-target"))
	wd := filepath.Join(alias, "clients", "browser", hex.EncodeToString(sum[:16]))
	if want := daemon.SocketPath(alias, wd); sock != want {
		t.Fatalf("first-start socket %s differs from canonical %s", sock, want)
	}
}
