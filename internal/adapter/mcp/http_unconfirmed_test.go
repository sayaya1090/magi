package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func TestHTTPTransportUnconfirmedCallScenarios(t *testing.T) {
	t.Run("connection closed before response", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking not supported", http.StatusInternalServerError)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			conn.Close()
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("error %q does not contain unconfirmedCallNotice", err.Error())
		}
		var unconf *unconfirmedCallError
		if !errors.As(err, &unconf) {
			t.Errorf("expected error to be *unconfirmedCallError, got %T", err)
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server to be called exactly 1 time (no retries), got %d", count)
		}
	})

	t.Run("HTTP 500 error", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			http.Error(w, "internal server crash", http.StatusInternalServerError)
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("error %q does not contain unconfirmedCallNotice", err.Error())
		}
		if !strings.Contains(err.Error(), "internal server crash") {
			t.Errorf("error %q does not contain original HTTP error body", err.Error())
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server to be called exactly 1 time, got %d", count)
		}
	})

	t.Run("truncated JSON response", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"res`))
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("error %q does not contain unconfirmedCallNotice", err.Error())
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server to be called exactly 1 time, got %d", count)
		}
	})

	t.Run("mismatched response ID", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"jsonrpc":"2.0","id":999999,"result":{"content":[{"type":"text","text":"wrong id"}]}}`))
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("error %q does not contain unconfirmedCallNotice", err.Error())
		}
		if !strings.Contains(err.Error(), "response id mismatch") {
			t.Errorf("error %q does not mention response id mismatch", err.Error())
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server to be called exactly 1 time, got %d", count)
		}
	})

	t.Run("missing result and error in JSON response", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			var req request
			json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Neither result nor error
			w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d}`, req.ID)))
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("error %q does not contain unconfirmedCallNotice", err.Error())
		}
		if !strings.Contains(err.Error(), "response must contain either result or error") {
			t.Errorf("error %q does not mention missing result or error", err.Error())
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server to be called exactly 1 time, got %d", count)
		}
	})

	t.Run("both result and error present in JSON response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req request
			json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{},"error":{"code":-32000,"message":"err"}}`, req.ID)))
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("error %q does not contain unconfirmedCallNotice", err.Error())
		}
		if !strings.Contains(err.Error(), "response must contain either result or error") {
			t.Errorf("error %q does not mention ambiguity between result and error", err.Error())
		}
	})

	t.Run("SSE stream aborts before response", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if ok {
				flusher.Flush()
			}
			// Close connection by hijacking or simply returning without sending event
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("error %q does not contain unconfirmedCallNotice", err.Error())
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server to be called exactly 1 time, got %d", count)
		}
	})

	t.Run("valid result returns cleanly without notice", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req request
			json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			res := callToolResult{Content: []contentBlock{{Type: "text", Text: "operation completed successfully"}}}
			rawRes, _ := json.Marshal(res)
			json.NewEncoder(w).Encode(message{
				JSONRPC: jsonRPCVersion,
				ID:      &req.ID,
				Result:  rawRes,
			})
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		res, err := client.CallTool(ctx, "test_tool", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Content) != 1 || res.Content[0].Text != "operation completed successfully" {
			t.Errorf("unexpected result content: %+v", res.Content)
		}
	})

	t.Run("authoritative JSON-RPC error response is NOT wrapped in unconfirmed notice", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			var req request
			json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(message{
				JSONRPC: jsonRPCVersion,
				ID:      &req.ID,
				Error: &rpcError{
					Code:    -32601,
					Message: "Method not found",
				},
			})
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("authoritative RPC error should NOT contain unconfirmedCallNotice, got: %q", err.Error())
		}
		if !strings.Contains(err.Error(), "Method not found") {
			t.Errorf("error %q should contain server error message", err.Error())
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server call count 1, got %d", count)
		}
	})
}

func TestHTTPTransportCancellationBoundaries(t *testing.T) {
	t.Run("pre-dispatch cancellation makes 0 server calls and does not wrap unconfirmed notice", func(t *testing.T) {
		var callCount atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before calling CallTool

		_, err := client.CallTool(ctx, "test_tool", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("pre-dispatch cancellation must NOT wrap unconfirmed notice: %q", err.Error())
		}
		if count := callCount.Load(); count != 0 {
			t.Errorf("expected server calls 0, got %d", count)
		}
	})

	t.Run("post-dispatch cancellation produces unconfirmed error and preserves errors.Is context.Canceled", func(t *testing.T) {
		var callCount atomic.Int32
		serverStarted := make(chan struct{})
		serverRelease := make(chan struct{})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			close(serverStarted)
			<-serverRelease
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithCancel(context.Background())

		errCh := make(chan error, 1)
		go func() {
			_, err := client.CallTool(ctx, "test_tool", nil)
			errCh <- err
		}()

		<-serverStarted // Confirm request reached the server
		cancel()        // Cancel context while waiting for response
		close(serverRelease)

		err := <-errCh
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("expected post-dispatch cancel error to contain unconfirmedCallNotice, got: %q", err.Error())
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected errors.Is(err, context.Canceled) to be true, got %v", err)
		}
		var unconf *unconfirmedCallError
		if !errors.As(err, &unconf) {
			t.Errorf("expected error to be *unconfirmedCallError, got %T", err)
		}
		if count := callCount.Load(); count != 1 {
			t.Errorf("expected server calls 1, got %d", count)
		}
	})
}

func TestMCPToolExecuteUnconfirmedIntegration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server died mid-turn", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newHTTPClient(srv.URL, nil, nil)
	defer client.Close()

	tool := &mcpTool{
		name:    "mcp__server__remote_op",
		remote:  "remote_op",
		byOwner: map[string]*Client{"": client},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := tool.Execute(ctx, json.RawMessage("{}"), port.ToolEnv{SessionID: "s1"})
	if err != nil {
		t.Fatalf("tool.Execute returned Go error %v, expected error wrapped inside ToolResult", err)
	}
	if !res.IsError {
		t.Errorf("expected ToolResult.IsError to be true, got false")
	}
	var contentStr string
	if err := json.Unmarshal(res.Content, &contentStr); err != nil {
		t.Fatalf("failed to unmarshal content string: %v", err)
	}
	if !strings.Contains(contentStr, unconfirmedCallNotice) {
		t.Errorf("expected content string to contain unconfirmedCallNotice, got %q", contentStr)
	}
}

func TestNonToolCallMethodsDoNotWrapUnconfirmedNotice(t *testing.T) {
	t.Run("HTTP initialize failure does not wrap unconfirmed notice", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "initialize refused", http.StatusInternalServerError)
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := client.Initialize(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var uce *unconfirmedCallError
		if errors.As(err, &uce) {
			t.Errorf("Initialize failure should NOT be unconfirmedCallError: %v", err)
		}
		if strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("Initialize failure should NOT contain unconfirmedCallNotice, got: %q", err.Error())
		}
	})

	t.Run("HTTP tools/list failure does not wrap unconfirmed notice", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "list refused", http.StatusInternalServerError)
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := client.ListTools(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var uce *unconfirmedCallError
		if errors.As(err, &uce) {
			t.Errorf("ListTools failure should NOT be unconfirmedCallError: %v", err)
		}
		if strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("ListTools failure should NOT contain unconfirmedCallNotice, got: %q", err.Error())
		}
	})

	t.Run("Non-tool call with result:null + error does not wrap unconfirmed notice", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req request
			json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":null,"error":{"code":-32000,"message":"err"}}`, req.ID)))
		}))
		defer srv.Close()

		client := newHTTPClient(srv.URL, nil, nil)
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := client.Initialize(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var uce *unconfirmedCallError
		if errors.As(err, &uce) {
			t.Errorf("Initialize with abnormal response should NOT be unconfirmedCallError: %v", err)
		}
		if strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("Initialize failure should NOT contain unconfirmedCallNotice, got: %q", err.Error())
		}
	})
}

func TestHTTPTransportFieldPresenceAndAuthoritativeScenarios(t *testing.T) {
	scenarios := []struct {
		name          string
		body          string // format string with %d for request ID
		isSSE         bool
		expectError   bool
		isUnconfirmed bool
		expectedMsg   string
	}{
		{
			name:          "JSON: result:null + error -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"result":null,"error":{"code":-32000,"message":"x"}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "response must contain either result or error",
		},
		{
			name:          "JSON: result:null alone -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"result":null}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "tools/call returned null result",
		},
		{
			name:          "JSON: error:null alone -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":null}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "response error field is null",
		},
		{
			name:          "JSON: valid result -> clean success",
			body:          `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"ok"}]}}`,
			isSSE:         false,
			expectError:   false,
			isUnconfirmed: false,
		},
		{
			name:          "JSON: valid error -> authoritative error, not unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":-32601,"message":"tool not found"}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: false,
			expectedMsg:   "tool not found",
		},
		// §6.44.9 RPC error completeness test cases (JSON)
		{
			name:          "JSON: error missing code -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"message":"x"}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "missing 'code'",
		},
		{
			name:          "JSON: error missing message -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":-32000}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "missing 'message'",
		},
		{
			name:          "JSON: error code is null -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":null,"message":"x"}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error code is null",
		},
		{
			name:          "JSON: error message is null -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":null}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error message is null",
		},
		{
			name:          "JSON: error code is string -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":"-32000","message":"x"}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error code is not a number",
		},
		{
			name:          "JSON: error code is float -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":-32000.5,"message":"x"}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error code must be an integer",
		},
		{
			name:          "JSON: error message is number -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":123}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error message must be a string",
		},
		{
			name:          "JSON: error is empty object -> unconfirmed",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "missing 'code'",
		},
		{
			name:          "JSON: error with code=0 and message=\"\" is valid authoritative error",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":0,"message":""}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: false,
		},
		{
			name:          "JSON: error with negative code, message, and data is valid authoritative error",
			body:          `{"jsonrpc":"2.0","id":%d,"error":{"code":-32601,"message":"Method not found","data":{"detail":"more"}}}`,
			isSSE:         false,
			expectError:   true,
			isUnconfirmed: false,
			expectedMsg:   "Method not found",
		},
		{
			name:          "SSE: result:null + error -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":null,\"error\":{\"code\":-32000,\"message\":\"x\"}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "response must contain either result or error",
		},
		{
			name:          "SSE: result:null alone -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":null}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "tools/call returned null result",
		},
		{
			name:          "SSE: error:null alone -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":null}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "response error field is null",
		},
		{
			name:          "SSE: valid result -> clean success",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n\n",
			isSSE:         true,
			expectError:   false,
			isUnconfirmed: false,
		},
		{
			name:          "SSE: valid error -> authoritative error, not unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32601,\"message\":\"tool not found\"}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: false,
			expectedMsg:   "tool not found",
		},
		// §6.44.9 RPC error completeness test cases (SSE)
		{
			name:          "SSE: error missing code -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"message\":\"x\"}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "missing 'code'",
		},
		{
			name:          "SSE: error missing message -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32000}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "missing 'message'",
		},
		{
			name:          "SSE: error code is null -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":null,\"message\":\"x\"}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error code is null",
		},
		{
			name:          "SSE: error message is null -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32000,\"message\":null}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error message is null",
		},
		{
			name:          "SSE: error code is string -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":\"-32000\",\"message\":\"x\"}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error code is not a number",
		},
		{
			name:          "SSE: error code is float -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32000.5,\"message\":\"x\"}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error code must be an integer",
		},
		{
			name:          "SSE: error message is number -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32000,\"message\":123}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "error message must be a string",
		},
		{
			name:          "SSE: error is empty object -> unconfirmed",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: true,
			expectedMsg:   "missing 'code'",
		},
		{
			name:          "SSE: error with code=0 and message=\"\" is valid authoritative error",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":0,\"message\":\"\"}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: false,
		},
		{
			name:          "SSE: error with negative code, message, and data is valid authoritative error",
			body:          "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32601,\"message\":\"Method not found\",\"data\":{\"detail\":\"more\"}}}\n\n",
			isSSE:         true,
			expectError:   true,
			isUnconfirmed: false,
			expectedMsg:   "Method not found",
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req request
				json.NewDecoder(r.Body).Decode(&req)
				if sc.isSSE {
					w.Header().Set("Content-Type", "text/event-stream")
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, sc.body, req.ID)
			}))
			defer srv.Close()

			client := newHTTPClient(srv.URL, nil, nil)
			defer client.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			res, err := client.CallTool(ctx, "test_tool", nil)
			if sc.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil result: %+v", res)
				}
				var uce *unconfirmedCallError
				isUce := errors.As(err, &uce)
				if isUce != sc.isUnconfirmed {
					t.Errorf("errors.As(unconfirmedCallError) = %v, expected %v (err: %v)", isUce, sc.isUnconfirmed, err)
				}
				hasNotice := strings.Contains(err.Error(), unconfirmedCallNotice)
				if hasNotice != sc.isUnconfirmed {
					t.Errorf("strings.Contains(unconfirmedCallNotice) = %v, expected %v (err: %v)", hasNotice, sc.isUnconfirmed, err)
				}
				if sc.expectedMsg != "" && !strings.Contains(err.Error(), sc.expectedMsg) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), sc.expectedMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(res.Content) != 1 || res.Content[0].Text != "ok" {
					t.Errorf("unexpected content: %+v", res.Content)
				}
			}
		})
	}
}

func TestParseHTTPRPCErrorDirectUnit(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		expectError  bool
		expectedCode int
		expectedMsg  string
	}{
		{
			name:        "missing code",
			raw:         `{"message":"x"}`,
			expectError: true,
		},
		{
			name:        "missing message",
			raw:         `{"code":-32000}`,
			expectError: true,
		},
		{
			name:        "null code",
			raw:         `{"code":null,"message":"x"}`,
			expectError: true,
		},
		{
			name:        "null message",
			raw:         `{"code":-32000,"message":null}`,
			expectError: true,
		},
		{
			name:        "string code",
			raw:         `{"code":"-32000","message":"x"}`,
			expectError: true,
		},
		{
			name:        "float code",
			raw:         `{"code":-32000.5,"message":"x"}`,
			expectError: true,
		},
		{
			name:        "number message",
			raw:         `{"code":-32000,"message":123}`,
			expectError: true,
		},
		{
			name:        "empty object",
			raw:         `{}`,
			expectError: true,
		},
		{
			name:         "code=0 message=\"\"",
			raw:          `{"code":0,"message":""}`,
			expectError:  false,
			expectedCode: 0,
			expectedMsg:  "",
		},
		{
			name:         "code=-32601 message=\"not found\" and data",
			raw:          `{"code":-32601,"message":"not found","data":{"extra":true}}`,
			expectError:  false,
			expectedCode: -32601,
			expectedMsg:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcErr, err := parseHTTPRPCError([]byte(tt.raw))
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil (rpcErr: %+v)", rpcErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if rpcErr.Code != tt.expectedCode {
					t.Errorf("Code = %d, expected %d", rpcErr.Code, tt.expectedCode)
				}
				if rpcErr.Message != tt.expectedMsg {
					t.Errorf("Message = %q, expected %q", rpcErr.Message, tt.expectedMsg)
				}
			}
		})
	}
}
