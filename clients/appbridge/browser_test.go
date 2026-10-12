package appbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func browserFixture(t *testing.T, call func(context.Context, string, string, json.RawMessage) (any, error)) (*Server, Binding, string) {
	t.Helper()
	socket := mockDaemon(t, func(p map[string]any) any {
		if p["method"] == "sessions" {
			return map[string]any{"ok": true, "sessions": []any{map[string]any{"id": "source"}}}
		}
		return map[string]any{"ok": true}
	})
	s, err := New(Config{App: "browser", Token: "test", Tools: []Tool{{Name: "read", InputSchema: map[string]any{"type": "object"}}}, Call: call, ExternalSocketAllowed: func(path string) bool { return path == socket }})
	if err != nil {
		t.Fatal(err)
	}
	parent := Binding{ID: "parent", Session: "ui", Target: "chrome", socket: socket, pending: make(chan *toolCall, 8), lastPoll: time.Now()}
	s.bindings[parent.ID] = &parent
	return s, parent, socket
}
func browserPost(s *Server, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	s.Mux.ServeHTTP(w, r)
	return w
}
func TestBrowserExternalProxyOwnerAndExpiry(t *testing.T) {
	s, parent, socket := browserFixture(t, func(_ context.Context, target, name string, args json.RawMessage) (any, error) {
		return map[string]string{"target": target, "tool": name}, nil
	})
	for _, p := range []struct{ socket, session string }{{"/tmp/arbitrary.sock", "source"}, {socket, "missing"}} {
		if _, _, err := s.AttachBrowser(context.Background(), parent.ID, p.socket, p.session, true); err == nil {
			t.Fatal("unpublished socket or unknown session accepted")
		}
	}
	lease, _, err := s.AttachBrowser(context.Background(), parent.ID, socket, "source", true)
	if err != nil {
		t.Fatal(err)
	}
	body := func(session string) string {
		b, _ := json.Marshal(map[string]any{"lease": lease.ID, "socket": socket, "session": session, "name": "read", "args": map[string]any{}})
		return string(b)
	}
	if w := browserPost(s, "/api/browser/call", body("other")); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := browserPost(s, "/api/browser/call", body("source")); w.Code != 200 || !strings.Contains(w.Body.String(), lease.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	s.mu.Lock()
	s.bindings[lease.ID].expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if w := browserPost(s, "/api/browser/call", body("source")); w.Code != 400 {
		t.Fatal("expired lease accepted")
	}
}
func TestBrowserRelayUsesUIQueueAndRejectsLateResult(t *testing.T) {
	s, parent, socket := browserFixture(t, nil)
	lease, _, err := s.AttachBrowser(context.Background(), parent.ID, socket, "source", true)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.relay(context.Background(), lease, "read", json.RawMessage(`{}`)); done <- err }()
	var c *toolCall
	select {
	case c = <-parent.pending:
	case <-time.After(time.Second):
		t.Fatal("relay did not reach Chrome queue")
	}
	if c.Session != "ui" || c.SourceSession != "source" || c.Lease != lease.ID || c.binding != parent.ID {
		t.Fatalf("wrong attribution: %+v", c)
	}
	if err := s.Disconnect(context.Background(), parent.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("parent disconnect did not cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("request leaked")
	}
	b, _ := json.Marshal(map[string]any{"binding": parent.ID, "session": "ui", "callId": c.ID, "result": "late"})
	if w := browserPost(s, "/api/browser/result", string(b)); w.Code != 409 {
		t.Fatal("late result accepted", w.Code)
	}
}
func TestBrowserRelayResultAndCancellation(t *testing.T) {
	s, parent, socket := browserFixture(t, nil)
	lease, _, err := s.AttachBrowser(context.Background(), parent.ID, socket, "source", true)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		result, err := s.relay(context.Background(), lease, "read", json.RawMessage(`{}`))
		if err == nil && result != "raw" {
			t.Error(result)
		}
		done <- err
	}()
	c := <-parent.pending
	b, _ := json.Marshal(map[string]any{"binding": parent.ID, "session": "ui", "callId": c.ID, "result": "raw"})
	if w := browserPost(s, "/api/browser/result", string(b)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.relay(ctx, lease, "read", json.RawMessage(`{}`)); err == nil {
		t.Fatal("cancelled request accepted")
	}
	s.mu.Lock()
	n := len(s.calls)
	s.mu.Unlock()
	if n != 0 {
		t.Fatal("call leaked")
	}
}
func TestBrowserDirectBackendRejectsConcurrentCalls(t *testing.T) {
	entered := make(chan struct{})
	finish := make(chan struct{})
	s, parent, socket := browserFixture(t, func(context.Context, string, string, json.RawMessage) (any, error) {
		close(entered)
		<-finish
		return "ok", nil
	})
	lease, _, err := s.AttachBrowser(context.Background(), parent.ID, socket, "source", true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"lease": lease.ID, "socket": socket, "session": "source", "name": "read", "args": map[string]any{}})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- browserPost(s, "/api/browser/call", string(b)) }()
	<-entered
	if w := browserPost(s, "/api/browser/call", string(b)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "처리 중") {
		t.Fatal(w.Code, w.Body.String())
	}
	close(finish)
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestBrowserCheckTracksLeaseRevocationAndReleasesBackend(t *testing.T) {
	s, parent, socket := browserFixture(t, nil)
	var released []string
	s.Config.Release = func(id string) error { released = append(released, id); return nil }
	lease, _, err := s.AttachBrowser(context.Background(), parent.ID, socket, "source", true)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.relay(context.Background(), lease, "read", json.RawMessage(`{}`)); done <- err }()
	var call *toolCall
	select {
	case call = <-parent.pending:
	case <-time.After(time.Second):
		t.Fatal("missing call")
	}
	payload, _ := json.Marshal(map[string]any{"binding": parent.ID, "session": parent.Session, "callId": call.ID})
	if w := browserPost(s, "/api/browser/check", string(payload)); w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Fatal(w.Body.String())
	}
	if err = s.Disconnect(context.Background(), lease.ID); err != nil {
		t.Fatal(err)
	}
	if w := browserPost(s, "/api/browser/check", string(payload)); w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":false`) {
		t.Fatal("revoked call remained valid", w.Body.String())
	}
	if len(released) != 1 || released[0] != lease.ID {
		t.Fatal("wrong profile released", released)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked call succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("revoked relay not cancelled")
	}
}

func TestBrowserPageErrorDoesNotDestroyLease(t *testing.T) {
	attempts := 0
	s, parent, socket := browserFixture(t, func(context.Context, string, string, json.RawMessage) (any, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("stale snapshot")
		}
		return map[string]string{"text": "still same profile"}, nil
	})
	lease, _, err := s.AttachBrowser(context.Background(), parent.ID, socket, "source", true)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"lease": lease.ID, "socket": socket, "session": "source", "name": "read", "args": map[string]any{}})
	first := browserPost(s, "/api/browser/call", string(payload))
	if first.Code != 200 || !strings.Contains(first.Body.String(), "stale snapshot") {
		t.Fatal(first.Body.String())
	}
	next := browserPost(s, "/api/browser/call", string(payload))
	if next.Code != 200 || !strings.Contains(next.Body.String(), "still same profile") {
		t.Fatal(next.Body.String())
	}
}
