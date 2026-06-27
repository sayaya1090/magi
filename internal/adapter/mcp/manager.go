package mcp

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/sayaya1090/magi/internal/port"
)

// ToolSink is the subset of a tool registry the manager needs (satisfied by
// *builtin.Registry).
type ToolSink interface {
	Register(port.Tool)
	Unregister(name string)
}

// Manager owns running MCP servers and keeps their tools registered in a shared
// sink. When a server exits, its tools are unregistered automatically (F-MCP).
type Manager struct {
	sink ToolSink

	mu      sync.Mutex
	servers map[string]*serverConn
}

type serverConn struct {
	name   string
	client *Client
	cmd    *exec.Cmd
	tools  []string
}

// NewManager returns a manager that registers tools into sink.
func NewManager(sink ToolSink) *Manager {
	return &Manager{sink: sink, servers: map[string]*serverConn{}}
}

// AddStdio spawns an MCP server over stdio, performs the handshake, discovers
// its tools, and registers them. The server's tools are removed if the process exits.
func (m *Manager) AddStdio(ctx context.Context, name, command string, args, env []string) error {
	cmd := exec.Command(command, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp: start %q: %w", command, err)
	}

	client := newClient(stdout, stdin, &procCloser{stdin: stdin, cmd: cmd})
	return m.registerClient(ctx, name, client, cmd)
}

// AddHTTP connects to an MCP server via HTTP transport (Streamable HTTP),
// performs the handshake, discovers its tools, and registers them.
func (m *Manager) AddHTTP(ctx context.Context, name, url string, headers map[string]string) error {
	client := newHTTPClient(url, headers, nil)
	return m.registerClient(ctx, name, client, nil)
}

// AddHTTPDynamic is like AddHTTP but takes a headers function evaluated fresh on
// every request, so the caller can inject runtime values (current time, model,
// auth tokens) that change between requests rather than being frozen at setup.
func (m *Manager) AddHTTPDynamic(ctx context.Context, name, url string, headersFn func() map[string]string) error {
	client := newHTTPClient(url, nil, headersFn)
	return m.registerClient(ctx, name, client, nil)
}

// registerClient is the common logic for registering a client (stdio or HTTP).
func (m *Manager) registerClient(ctx context.Context, name string, client *Client, cmd *exec.Cmd) error {
	if err := client.Initialize(ctx); err != nil {
		client.Close()
		return fmt.Errorf("mcp: initialize %q: %w", name, err)
	}
	defs, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		return fmt.Errorf("mcp: list tools %q: %w", name, err)
	}

	sc := &serverConn{name: name, client: client, cmd: cmd}
	for _, d := range defs {
		schema := d.InputSchema
		if len(schema) == 0 {
			schema = []byte(`{"type":"object"}`)
		}
		t := &mcpTool{client: client, name: d.Name, description: d.Description, schema: schema}
		m.sink.Register(t)
		sc.tools = append(sc.tools, d.Name)
	}

	m.mu.Lock()
	m.servers[name] = sc
	m.mu.Unlock()

	// Unregister tools when the server goes away.
	go func() {
		<-client.Done()
		m.Remove(name)
	}()
	return nil
}

// Remove unregisters a server's tools and stops it.
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	sc := m.servers[name]
	delete(m.servers, name)
	m.mu.Unlock()
	if sc == nil {
		return
	}
	for _, t := range sc.tools {
		m.sink.Unregister(t)
	}
	sc.client.Close()
}

// Close stops all servers.
func (m *Manager) Close() {
	m.mu.Lock()
	names := make([]string, 0, len(m.servers))
	for n := range m.servers {
		names = append(names, n)
	}
	m.mu.Unlock()
	for _, n := range names {
		m.Remove(n)
	}
}

// procCloser closes the server's stdin and kills the process.
type procCloser struct {
	stdin io.Closer
	cmd   *exec.Cmd
}

func (p *procCloser) Close() error {
	p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}
