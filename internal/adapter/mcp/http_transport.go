package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/httpx"
)

const unconfirmedCallNotice = "MCP_RESULT_UNKNOWN: no complete tool response was received; the operation may have run. Do not automatically retry a write; inspect the target application before retrying."

// unconfirmedCallError wraps errors that occur after HTTP request dispatch for tools/call (§6.44.4).
type unconfirmedCallError struct {
	cause error
}

func (e *unconfirmedCallError) Error() string {
	if e.cause == nil {
		return unconfirmedCallNotice
	}
	return fmt.Sprintf("%s: %s", e.cause.Error(), unconfirmedCallNotice)
}

func (e *unconfirmedCallError) Unwrap() error {
	return e.cause
}

// httpTransport implements MCP Streamable HTTP transport.
// Specification: https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
// The revision this client advertises is older — see protocolVersion in jsonrpc.go — so where the
// two could differ, the advertised one governs and is the one cited at the point of use.
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
	// unreachable counts calls in a row that never reached the server. An HTTP endpoint has no
	// moment of death to notice, so this is the substitute — see missed().
	unreachable int
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
// reachable asks whether anybody is home at the endpoint, with one raw POST of a "ping". Only
// "nobody was there" is a no: a refusal, an RPC error, junk — any HTTP response at all — is
// somebody answering, and a probe that ran out of its own deadline says we stopped waiting, not
// that there was nobody to wait for (the same line call draws below).
func (t *httpTransport) reachable(ctx context.Context) bool {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return false
	}
	t.nextID++
	id := t.nextID
	t.mu.Unlock()
	body, err := json.Marshal(request{JSONRPC: jsonRPCVersion, ID: id, Method: "ping"})
	if err != nil {
		return true // our own marshalling says nothing about the server
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.endpoint, bytes.NewReader(body))
	if err != nil {
		return true
	}
	t.applyHeaders(httpReq, "application/json, text/event-stream")
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return ctx.Err() != nil // we gave up is not the same fact as nobody home
	}
	resp.Body.Close()
	return true
}

func (t *httpTransport) wrapUnconfirmed(method string, err error) error {
	if err == nil {
		return nil
	}
	if method != "tools/call" {
		return err
	}
	return &unconfirmedCallError{cause: err}
}

// httpMessage represents a decoded JSON-RPC response in the HTTP transport (§6.44.6).
// Result and Error are raw JSON so that field presence (including explicit null) can be distinguished
// from field omission.
type httpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// validateHTTPResponse validates JSON-RPC response fields and extracts the result or error (§6.44.6).
// It returns an *rpcError if the response contains an authoritative error object from the server,
// which callers must NOT wrap in unconfirmedCallError.
// If the response violates protocol constraints (e.g., ID mismatch, version mismatch, ambiguous result/error,
// null result for tools/call, malformed error object), it returns a protocol error that is wrapped for tools/call.
func validateHTTPResponse(msg *httpMessage, expectedID int64, method string, out any) error {
	if msg.JSONRPC != jsonRPCVersion {
		return fmt.Errorf("mcp: invalid jsonrpc version: %q", msg.JSONRPC)
	}
	if msg.ID == nil || *msg.ID != expectedID {
		return fmt.Errorf("mcp: response id mismatch: expected %d, got %v", expectedID, msg.ID)
	}

	hasResult := len(msg.Result) > 0
	hasError := len(msg.Error) > 0

	if (hasResult && hasError) || (!hasResult && !hasError) {
		return fmt.Errorf("mcp: response must contain either result or error")
	}

	if hasError {
		if bytes.Equal(bytes.TrimSpace(msg.Error), []byte("null")) {
			return fmt.Errorf("mcp: response error field is null")
		}
		var rpcErr rpcError
		if err := json.Unmarshal(msg.Error, &rpcErr); err != nil {
			return fmt.Errorf("mcp: bad error object: %w", err)
		}
		if rpcErr.Code == 0 && rpcErr.Message == "" {
			return fmt.Errorf("mcp: incomplete error object")
		}
		return &rpcErr
	}

	// hasResult is true
	if bytes.Equal(bytes.TrimSpace(msg.Result), []byte("null")) {
		if method == "tools/call" {
			return fmt.Errorf("mcp: tools/call returned null result")
		}
		if out != nil {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}

	if out != nil {
		if err := json.Unmarshal(msg.Result, out); err != nil {
			return fmt.Errorf("mcp: unmarshal result: %w", err)
		}
	}
	return nil
}

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

	// If context was cancelled before Do, return the context error directly without unconfirmed wrapping.
	if err := ctx.Err(); err != nil {
		return err
	}

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		// Only when NOBODY was there. Do also fails when this side gave up — the per-call deadline
		// in tool.go, or a person interrupting the turn — and neither says anything about the
		// server. Measured on the first version of this: a live server that took longer than the
		// deadline was reached three times and had its tools taken away anyway, and three
		// interrupted turns closed a transport nothing had even dialled. The client carries no
		// Timeout of its own, so every deadline arrives through ctx and this one line separates
		// "we stopped waiting" from "there is nobody to wait for".
		if ctx.Err() == nil {
			t.missed()
		}
		return t.wrapUnconfirmed(method, err)
	}
	defer resp.Body.Close()
	t.captureSession(resp)

	if resp.StatusCode != http.StatusOK {
		t.answered() // a refusal is the server speaking: somebody is home to refuse
		// Surface a bounded snippet of the error body — servers often explain why.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var httpErr error
		if s := strings.TrimSpace(string(snippet)); s != "" {
			httpErr = fmt.Errorf("mcp: http %d: %s: %s", resp.StatusCode, resp.Status, s)
		} else {
			httpErr = fmt.Errorf("mcp: http %d: %s", resp.StatusCode, resp.Status)
		}
		return t.wrapUnconfirmed(method, httpErr)
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		err := t.readSSEStream(ctx, resp.Body, id, method, out)
		if err == nil {
			t.answered()
			return nil
		}
		var rpcErr *rpcError
		if errors.As(err, &rpcErr) {
			t.answered()
			return fmt.Errorf("mcp: %s: %s", method, rpcErr.Message)
		}
		// A stream that broke halfway neither resets the streak nor adds to it: the headers came
		// from somewhere, so it is not "nobody home", and the break may be ours (a cancelled turn
		// closes the body from this side).
		return t.wrapUnconfirmed(method, err)
	}

	// Single JSON response. The streak resets on a message that ARRIVED WHOLE — including one
	// carrying the server's own error, which is still the server talking. Resetting on the response
	// headers (where this used to be) meant a helper that accepted every call and died mid-body was
	// never dropped: each half-answer wiped the count of the ones before it.
	var msg httpMessage
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return t.wrapUnconfirmed(method, err)
	}
	t.answered()

	if err := validateHTTPResponse(&msg, id, method, out); err != nil {
		var rpcErr *rpcError
		if errors.As(err, &rpcErr) {
			return fmt.Errorf("mcp: %s: %s", method, rpcErr.Message)
		}
		return t.wrapUnconfirmed(method, err)
	}
	return nil
}

// readSSEStream processes Server-Sent Events until the message answering our
// request id arrives. Cancellation is honored both via the request context
// (which closes the body, unblocking the scanner) and an explicit ctx check.
func (t *httpTransport) readSSEStream(ctx context.Context, r io.Reader, id int64, method string, out any) error {
	scanner := bufio.NewScanner(r)
	// One SSE line has to hold a whole JSON-RPC message, and a message answering with a picture
	// carries it base64'd — a third larger than the file. This used to be a flat 1MB while
	// imageCap said 8MB, so a picture over roughly 750KB died here as a scanner error: two limits
	// that disagreed, and the smaller one was the invisible one. It is now derived from the cap
	// that is written down, plus room for the JSON around it.
	scanner.Buffer(make([]byte, 0, 64*1024), sseFrameCap)
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
			var msg httpMessage
			if err := json.Unmarshal([]byte(data), &msg); err != nil {
				return fmt.Errorf("mcp: bad SSE event JSON: %w", err)
			}
			if msg.ID != nil && *msg.ID == id {
				return validateHTTPResponse(&msg, id, method, out)
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

	// 202 is the answer the spec asks for here, and it was the one status this rejected:
	// "If the server accepts the input, the server MUST return HTTP status code 202 Accepted
	// with no body" — and its sequence diagram answers notifications/initialized with exactly
	// that. So a server that followed the spec was turned away mid-handshake, and the ones that
	// attached were the ones that did not. 200 and 204 are not specified for this direction but
	// cost nothing to keep taking from servers that already send them.
	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusOK, http.StatusNoContent:
		return nil
	default:
		return fmt.Errorf("mcp: http %d: %s", resp.StatusCode, resp.Status)
	}
}

// unreachableStreak is how many calls in a row may fail to reach the server before this transport
// gives up and closes itself.
//
// Three, and only for calls that never reached anybody — a refused dial, a dropped connection.
// Not a call this side abandoned: our own deadline and a person's interrupt both look like a failed
// request here, and neither is evidence about the server.
// An HTTP status is not counted: a server that answers 500 is present and having a bad time, and
// dropping its tools mid-conversation would be this daemon deciding a running server is dead.
const unreachableStreak = 3

// missed records a call that could not reach the server, and closes the transport once nothing has
// answered three times running.
//
// Without this a tool server that is simply GONE stays attached forever. A stdio server dies and
// its pipe closes, which is what Done() reports and what unregisters its tools; an HTTP endpoint
// has no such moment — Done() closed only on an explicit Close(), so a helper that crashed left
// mcp__helper__* advertised to the model with nothing behind them, and its own restart was then
// refused because the name was still held by the dead registration.
//
// Not a health check on a timer: this counts the calls the daemon was making anyway. A server
// nobody is calling costs nothing by staying, and the first thing that touches it cleans it up.
func (t *httpTransport) missed() {
	t.mu.Lock()
	t.unreachable++
	gone := t.unreachable >= unreachableStreak && !t.closed
	t.mu.Unlock()
	if gone {
		t.Close()
	}
}

// answered resets the streak. One success means somebody is home.
func (t *httpTransport) answered() {
	t.mu.Lock()
	t.unreachable = 0
	t.mu.Unlock()
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
