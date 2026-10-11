package appbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func lifecycleServer(t *testing.T, failNew bool) (*Server, *[]string, *sync.Mutex) {
	var mu sync.Mutex
	requests := []string{}
	sessions := 0
	path := mockDaemon(t, func(p map[string]any) any {
		mu.Lock()
		defer mu.Unlock()
		method, _ := p["method"].(string)
		owner, _ := p["owner"].(string)
		requests = append(requests, method+":"+owner)
		switch method {
		case "sessions":
			return map[string]any{"ok": true, "sessions": []any{}}
		case "session-new":
			sessions++
			sid := "old"
			if sessions > 1 {
				sid = "new"
			}
			return map[string]any{"ok": true, "session": sid}
		case "mcp-attach":
			if failNew && owner == "new" {
				return map[string]any{"ok": false, "error": "test attach failure"}
			}
		}
		return map[string]any{"ok": true}
	})
	s, e := New(Config{App: "figma", Token: "test", Tools: []Tool{{Name: "figma_page"}}, Socket: func(context.Context, string) (string, error) { return path, nil }})
	if e != nil {
		t.Fatal(e)
	}
	return s, &requests, &mu
}
func TestFigmaSingleOwnerDisconnectAndReconnect(t *testing.T) {
	s, _, _ := lifecycleServer(t, false)
	b, e := s.Connect(context.Background(), "file-a")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Connect(context.Background(), "file-a"); e == nil {
		t.Fatal("duplicate Figma owner accepted")
	}
	done := make(chan error, 1)
	go func() { _, e := s.relay(context.Background(), b, "figma_page", json.RawMessage(`{}`)); done <- e }()
	select {
	case <-b.pending:
	case <-time.After(time.Second):
		t.Fatal("relay not queued")
	}
	r := httptest.NewRequest("POST", "/api/disconnect", strings.NewReader(`{"binding":"`+b.ID+`"}`))
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	s.Mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("pending call completed after disconnect")
		}
	case <-time.After(time.Second):
		t.Fatal("pending call not failed")
	}
	if _, e = s.binding(b.ID); e == nil {
		t.Fatal("binding retained")
	}
	next, e := s.Connect(context.Background(), "file-a")
	if e != nil || next.ID == b.ID {
		t.Fatalf("reconnect %v %#v", e, next)
	}
}
func TestSessionNewFailureRestoresOldToolsAndCancelsPending(t *testing.T) {
	s, requests, mu := lifecycleServer(t, true)
	b, e := s.Connect(context.Background(), "file-a")
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := s.relay(context.Background(), b, "figma_page", json.RawMessage(`{}`)); done <- e }()
	select {
	case <-b.pending:
	case <-time.After(time.Second):
		t.Fatal("no pending call")
	}
	if _, e = s.Request(context.Background(), b.ID, Request{Method: "new"}); e == nil {
		t.Fatal("attach failure hidden")
	}
	current, e := s.binding(b.ID)
	if e != nil || current.Session != "old" {
		t.Fatalf("rollback failed: %#v %v", current, e)
	}
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("old pending survived transition")
		}
	case <-time.After(time.Second):
		t.Fatal("pending not cancelled")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*requests) < 1 || (*requests)[len(*requests)-1] != "mcp-attach:old" {
		t.Fatalf("old tools not restored: %v", *requests)
	}
}
func TestOldQueuedSessionIsSkippedByPoll(t *testing.T) {
	s, _ := New(Config{App: "figma", Token: "test"})
	b := Binding{ID: "b", Session: "new", pending: make(chan *toolCall, 2)}
	s.bindings[b.ID] = &b
	old := &toolCall{ID: "old", binding: "b", Session: "old"}
	fresh := &toolCall{ID: "fresh", binding: "b", Session: "new"}
	s.calls[old.ID] = old
	s.calls[fresh.ID] = fresh
	b.pending <- old
	b.pending <- fresh
	r := httptest.NewRequest("POST", "/api/figma/poll", strings.NewReader(`{"binding":"b"}`))
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	s.Mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"callId":"fresh"`) {
		t.Fatal(w.Body.String())
	}
}
