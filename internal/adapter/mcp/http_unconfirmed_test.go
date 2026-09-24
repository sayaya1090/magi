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
		if strings.Contains(err.Error(), unconfirmedCallNotice) {
			t.Errorf("ListTools failure should NOT contain unconfirmedCallNotice, got: %q", err.Error())
		}
	})
}
