package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// transport abstracts the underlying protocol transport (stdio or HTTP).
type transport interface {
	call(ctx context.Context, method string, params any, out any) error
	notify(method string, params any) error
	Close() error
	Done() <-chan struct{}
}

// Client is a JSON-RPC client over an MCP transport (stdio or HTTP).
// It is safe for concurrent CallTool use; requests are correlated by id.
type Client struct {
	// stdio fields (deprecated in favor of transport interface)
	w   io.Writer
	r   *bufio.Reader
	cls io.Closer

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan message
	closed  bool

	done chan struct{}

	// transport abstraction (when set, overrides stdio fields)
	tr transport
}

// newClient wires a client to a stdio transport. Call Initialize before use.
func newClient(r io.Reader, w io.Writer, closer io.Closer) *Client {
	c := &Client{
		w:       w,
		r:       bufio.NewReaderSize(r, 1<<20),
		cls:     closer,
		pending: map[int64]chan message{},
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// newHTTPClient creates a client that uses HTTP transport. headersFn (may be
// nil) supplies per-request headers evaluated fresh on every call.
func newHTTPClient(endpoint string, headers map[string]string, headersFn func() map[string]string) *Client {
	tr := newHTTPTransport(endpoint, headers, headersFn)
	c := &Client{
		tr:   tr,
		done: make(chan struct{}),
	}
	// Forward the transport's done signal to the client's done channel
	go func() {
		<-tr.Done()
		close(c.done)
	}()
	return c
}

// readLoop reads newline-delimited messages and routes responses to waiters.
// Server-initiated requests/notifications are ignored in M4.
func (c *Client) readLoop() {
	defer close(c.done)
	for {
		line, err := c.r.ReadBytes('\n')
		if len(line) > 0 {
			var m message
			if json.Unmarshal(line, &m) == nil && m.ID != nil {
				c.mu.Lock()
				ch := c.pending[*m.ID]
				delete(c.pending, *m.ID)
				c.mu.Unlock()
				if ch != nil {
					ch <- m
				}
			}
		}
		if err != nil {
			c.failPending(err)
			return
		}
	}
}

func (c *Client) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		ch <- message{Error: &rpcError{Message: "transport closed: " + err.Error()}}
		delete(c.pending, id)
	}
}

// call sends a request and waits for its response (or ctx timeout).
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	// Delegate to transport if available (HTTP)
	if c.tr != nil {
		return c.tr.call(ctx, method, params, out)
	}

	// stdio transport (legacy)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("mcp: client closed")
	}
	c.nextID++
	id := c.nextID
	ch := make(chan message, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	if err := c.writeJSON(request{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: raw}); err != nil {
		return err
	}

	select {
	case m := <-ch:
		if m.Error != nil {
			return fmt.Errorf("mcp: %s: %s", method, m.Error.Message)
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

func (c *Client) notify(method string, params any) error {
	// Delegate to transport if available (HTTP)
	if c.tr != nil {
		return c.tr.notify(method, params)
	}

	// stdio transport (legacy)
	var raw json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		raw = b
	}
	return c.writeJSON(notification{JSONRPC: jsonRPCVersion, Method: method, Params: raw})
}

func (c *Client) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// Initialize performs the MCP handshake: initialize request + initialized notice.
func (c *Client) Initialize(ctx context.Context) error {
	params := initializeParams{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      clientInfo{Name: "magi", Version: "0.1.0"},
	}
	if err := c.call(ctx, "initialize", params, &struct{}{}); err != nil {
		return err
	}
	return c.notify("notifications/initialized", nil)
}

// ListTools returns the tools advertised by the server.
func (c *Client) ListTools(ctx context.Context) ([]toolDef, error) {
	var res listToolsResult
	if err := c.call(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// CallTool invokes a tool and returns its result.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (callToolResult, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var res callToolResult
	err := c.call(ctx, "tools/call", callToolParams{Name: name, Arguments: args}, &res)
	return res, err
}

// Close shuts down the transport.
func (c *Client) Close() error {
	// Delegate to transport if available (HTTP)
	if c.tr != nil {
		return c.tr.Close()
	}

	// stdio transport (legacy)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if c.cls != nil {
		return c.cls.Close()
	}
	return nil
}

// Done is closed when the read loop exits (server gone / transport closed).
func (c *Client) Done() <-chan struct{} { return c.done }
