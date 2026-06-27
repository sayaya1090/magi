package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/httpx"
)

// httpTransport implements MCP Streamable HTTP transport.
// Specification: https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
//
// Each call() is a self-contained HTTP request/response (the server may answer
// with a single application/json body or a text/event-stream), so unlike the
// stdio transport there is no background read loop or response-correlation map.
type httpTransport struct {
	endpoint   string
	httpClient *http.Client
	custom     *httpx.Headers // static + per-request custom headers (config / plugin)

	mu          sync.Mutex
	nextID      int64
	sessionID   string // Mcp-Session-Id assigned by the server, echoed on later requests
	lastEventID string // last SSE event id, sent as Last-Event-ID to resume a stream
	closed      bool
	done        chan struct{}
}

// newHTTPTransport creates a client for the MCP Streamable HTTP transport.
// headersFn (may be nil) is evaluated on every request so plugins can inject
// fresh runtime values (current time, model, …) — its results overlay headers.
func newHTTPTransport(endpoint string, headers map[string]string, headersFn func() map[string]string) *httpTransport {
	h := httpx.NewHeaders(headers)
	h.AddProvider(headersFn) // nil is ignored
	return &httpTransport{
		endpoint:   endpoint,
		httpClient: &http.Client{},
		custom:     h,
		done:       make(chan struct{}),
	}
}

// applyHeaders sets protocol, then custom, then session headers on a request.
// Custom (config/plugin) headers overlay the protocol ones; the server session
// id and SSE resume cursor are added last so a caller can't clobber them.
func (t *httpTransport) applyHeaders(req *http.Request, accept string) {
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	t.custom.Apply(req)
	t.mu.Lock()
	sid, last := t.sessionID, t.lastEventID
	t.mu.Unlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	if last != "" {
		req.Header.Set("Last-Event-ID", last)
	}
}

// captureSession records the server-assigned Mcp-Session-Id (sent back on the
// initialize response and reused for the lifetime of the connection).
func (t *httpTransport) captureSession(resp *http.Response) {
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}
}

// call sends a JSON-RPC request via HTTP POST and handles the response.
// The server may respond with either application/json (single response) or
// text/event-stream (SSE stream for multiple messages).
func (t *httpTransport) call(ctx context.Context, method string, params any, out any) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("mcp: transport closed")
	}
	t.nextID++
	id := t.nextID
	t.mu.Unlock()

	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}

	body, err := json.Marshal(request{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: raw})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	t.applyHeaders(httpReq, "application/json, text/event-stream")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	t.captureSession(resp)

	if resp.StatusCode != http.StatusOK {
		// Surface a bounded snippet of the error body — servers often explain why.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if s := strings.TrimSpace(string(snippet)); s != "" {
			return fmt.Errorf("mcp: http %d: %s: %s", resp.StatusCode, resp.Status, s)
		}
		return fmt.Errorf("mcp: http %d: %s", resp.StatusCode, resp.Status)
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return t.readSSEStream(ctx, resp.Body, id, out)
	}

	// Single JSON response.
	var msg message
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return err
	}
	if msg.Error != nil {
		return fmt.Errorf("mcp: %s: %s", method, msg.Error.Message)
	}
	if out != nil && len(msg.Result) > 0 {
		return json.Unmarshal(msg.Result, out)
	}
	return nil
}

// readSSEStream processes Server-Sent Events until the message answering our
// request id arrives. Cancellation is honored both via the request context
// (which closes the body, unblocking the scanner) and an explicit ctx check.
func (t *httpTransport) readSSEStream(ctx context.Context, r io.Reader, id int64, out any) error {
	scanner := bufio.NewScanner(r)
	// MCP frames can exceed bufio's 64KB default line size; allow up to 1MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var eventID, data string

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()

		if line == "" {
			// Blank line terminates an event.
			if data == "" {
				continue
			}
			if eventID != "" {
				t.mu.Lock()
				t.lastEventID = eventID
				t.mu.Unlock()
			}
			var msg message
			if err := json.Unmarshal([]byte(data), &msg); err != nil {
				return fmt.Errorf("mcp: bad SSE event JSON: %w", err)
			}
			if msg.ID != nil && *msg.ID == id {
				if msg.Error != nil {
					return fmt.Errorf("mcp: %s", msg.Error.Message)
				}
				if out != nil && len(msg.Result) > 0 {
					return json.Unmarshal(msg.Result, out)
				}
				return nil
			}
			// A server-initiated request/notification — not our response; skip.
			data = ""
			eventID = ""
			continue
		}

		switch {
		case strings.HasPrefix(line, "id:"):
			eventID = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "data:"):
			data += strings.TrimSpace(line[5:])
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("mcp: stream ended without response")
}

// notify sends a JSON-RPC notification (no response expected).
func (t *httpTransport) notify(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		raw = b
	}

	body, err := json.Marshal(notification{JSONRPC: jsonRPCVersion, Method: method, Params: raw})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", t.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	t.applyHeaders(httpReq, "")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	t.captureSession(resp)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("mcp: http %d: %s", resp.StatusCode, resp.Status)
	}
	return nil
}

// Close shuts down the transport.
func (t *httpTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.done)
	}
	return nil
}

// Done returns a channel that's closed when the transport is closed.
func (t *httpTransport) Done() <-chan struct{} {
	return t.done
}
