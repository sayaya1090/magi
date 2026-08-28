package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// fakeHTTPServer implements a minimal MCP server over HTTP for testing.
func fakeHTTPServer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Handle different methods
		switch req.Method {
		case "initialize":
			resp := message{
				JSONRPC: jsonRPCVersion,
				ID:      &req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{}}`),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case "tools/list":
			tools := listToolsResult{
				Tools: []toolDef{
					{Name: "http_echo", Description: "Echo over HTTP", InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)},
				},
			}
			result, _ := json.Marshal(tools)
			resp := message{
				JSONRPC: jsonRPCVersion,
				ID:      &req.ID,
				Result:  result,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case "tools/call":
			var params callToolParams
			json.Unmarshal(req.Params, &params)
			var args map[string]string
			json.Unmarshal(params.Arguments, &args)

			result := callToolResult{
				Content: []contentBlock{{Type: "text", Text: fmt.Sprintf("http_echo: %s", args["text"])}},
			}
			resultJSON, _ := json.Marshal(result)
			resp := message{
				JSONRPC: jsonRPCVersion,
				ID:      &req.ID,
				Result:  resultJSON,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		default:
			// Handle notifications (no response). 202 is what the spec tells a server to answer
			// here, so the double answers it — it used to send 204, which is why every test
			// passed while no conforming server could finish the handshake.
			if strings.HasPrefix(req.Method, "notifications/") {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}
}

// TestHTTPTransportInitializeAndList tests the HTTP transport handshake and tool listing.
func TestHTTPTransportInitializeAndList(t *testing.T) {
	srv := httptest.NewServer(fakeHTTPServer())
	defer srv.Close()

	client := newHTTPClient(srv.URL, nil, nil)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "http_echo" {
		t.Fatalf("tools=%+v want [http_echo]", tools)
	}
}

// TestHTTPTransportCallTool tests calling a tool over HTTP transport.
func TestHTTPTransportCallTool(t *testing.T) {
	srv := httptest.NewServer(fakeHTTPServer())
	defer srv.Close()

	client := newHTTPClient(srv.URL, nil, nil)
	defer client.Close()

	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	res, err := client.CallTool(ctx, "http_echo", json.RawMessage(`{"text":"hello http"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError || len(res.Content) != 1 || res.Content[0].Text != "http_echo: hello http" {
		t.Fatalf("result=%+v want 'http_echo: hello http'", res)
	}
}

// TestManagerHTTPTransport tests the Manager with HTTP transport.
func TestManagerHTTPTransport(t *testing.T) {
	srv := httptest.NewServer(fakeHTTPServer())
	defer srv.Close()

	reg := &testRegistry{tools: map[string]bool{}}
	mgr := NewManager(reg)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.AddHTTP(ctx, "http-server", srv.URL, nil); err != nil {
		t.Fatalf("AddHTTP: %v", err)
	}

	if !reg.tools["mcp__http-server__http_echo"] {
		t.Fatalf("namespaced http_echo tool not registered from HTTP MCP server; got %+v", reg.tools)
	}
}

// TestHTTPTransportCustomHeaders tests custom headers in HTTP transport.
func TestHTTPTransportCustomHeaders(t *testing.T) {
	var receivedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		fakeHTTPServer().ServeHTTP(w, r)
	}))
	defer srv.Close()

	headers := map[string]string{
		"X-Custom-Auth": "Bearer secret123",
		"X-Request-ID":  "test-request-id",
		"X-API-Version": "v2",
	}
	client := newHTTPClient(srv.URL, headers, nil)
	defer client.Close()

	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify custom headers were sent
	if receivedHeaders.Get("X-Custom-Auth") != "Bearer secret123" {
		t.Errorf("X-Custom-Auth = %q, want %q", receivedHeaders.Get("X-Custom-Auth"), "Bearer secret123")
	}
	if receivedHeaders.Get("X-Request-ID") != "test-request-id" {
		t.Errorf("X-Request-ID = %q, want %q", receivedHeaders.Get("X-Request-ID"), "test-request-id")
	}
	if receivedHeaders.Get("X-API-Version") != "v2" {
		t.Errorf("X-API-Version = %q, want %q", receivedHeaders.Get("X-API-Version"), "v2")
	}
}

// TestHTTPTransportDynamicHeaders verifies the per-request headers function is
// re-evaluated on every request (fresh values, not frozen) and reaches the wire.
func TestHTTPTransportDynamicHeaders(t *testing.T) {
	var seqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seqs = append(seqs, r.Header.Get("X-Seq"))
		fakeHTTPServer().ServeHTTP(w, r)
	}))
	defer srv.Close()

	n := 0
	headersFn := func() map[string]string {
		n++
		return map[string]string{"X-Seq": fmt.Sprintf("%d", n)}
	}
	client := newHTTPClient(srv.URL, nil, headersFn)
	defer client.Close()

	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	// Two requests (initialize, tools/list) → two distinct, increasing values.
	if len(seqs) < 2 || seqs[0] == seqs[1] {
		t.Fatalf("dynamic headers not re-evaluated per request: %v", seqs)
	}
}

// TestHTTPTransportSessionEcho verifies the server-assigned Mcp-Session-Id is
// captured from the initialize response and echoed on subsequent requests.
func TestHTTPTransportSessionEcho(t *testing.T) {
	const sid = "sess-xyz-123"
	var echoed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		echoed = append(echoed, r.Header.Get("Mcp-Session-Id"))
		w.Header().Set("Mcp-Session-Id", sid) // server assigns/echoes the session id
		fakeHTTPServer().ServeHTTP(w, r)
	}))
	defer srv.Close()

	client := newHTTPClient(srv.URL, nil, nil)
	defer client.Close()

	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	// First request has no session id yet; the second must echo what the server set.
	if len(echoed) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(echoed))
	}
	if echoed[0] != "" {
		t.Errorf("first request should not carry a session id, got %q", echoed[0])
	}
	if echoed[len(echoed)-1] != sid {
		t.Errorf("subsequent request should echo session id %q, got %q", sid, echoed[len(echoed)-1])
	}
}

// TestHTTPTransportErrorBody surfaces the server's error body in the returned error.
func TestHTTPTransportErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota exceeded for this token", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := newHTTPClient(srv.URL, nil, nil)
	defer client.Close()

	err := client.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected an error on 429")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error should surface the server body, got: %v", err)
	}
}

// testRegistry is a minimal registry for testing.
type testRegistry struct {
	tools map[string]bool
}

func (r *testRegistry) Register(t port.Tool) {
	r.tools[t.Name()] = true
}

func (r *testRegistry) Unregister(name string) {
	delete(r.tools, name)
}

// TestHTTPTransportNotificationStatuses pins which answers to a notification count as accepted.
//
// The one that matters is 202: the spec says a server that accepts a notification MUST answer with
// it, and this transport used to reject it, so conformance was the thing that failed to attach.
// 200 and 204 stay accepted because servers in the wild send them. A refusal must still be a
// refusal, and it must say which half of the handshake refused — the call site labels the whole
// thing "initialize", which sent readers to the request that had already succeeded.
func TestHTTPTransportNotificationStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		wantOK bool
	}{
		{http.StatusAccepted, true},
		{http.StatusOK, true},
		{http.StatusNoContent, true},
		{http.StatusBadRequest, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if strings.HasPrefix(req.Method, "notifications/") {
				w.WriteHeader(tc.status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(message{
				JSONRPC: jsonRPCVersion,
				ID:      &req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{}}`),
			})
		}))

		client := newHTTPClient(srv.URL, nil, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := client.Initialize(ctx)
		cancel()
		client.Close()
		srv.Close()

		if tc.wantOK && err != nil {
			t.Errorf("status %d: Initialize: %v", tc.status, err)
		}
		if !tc.wantOK {
			if err == nil {
				t.Errorf("status %d: Initialize succeeded, want refusal", tc.status)
			} else if !strings.Contains(err.Error(), "notifications/initialized") {
				t.Errorf("status %d: error %q does not name the step that failed", tc.status, err)
			}
		}
	}
}
