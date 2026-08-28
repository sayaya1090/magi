package mcp

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

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
	// Confine wraps a stdio server's argv so the OS runs it under this machine's sandbox. Supplied
	// by the binary that has the platform code; nil = spawn as-is.
	//
	// A stdio server is a program named in a config file and kept alive for the daemon's whole
	// life, and it was the last child spawned with no confinement while the bash tool beside it
	// had some. It is also the child most likely to be pointed at something outside the workspace
	// on purpose, so it is confined on the same terms as bash — turn the sandbox off and it runs
	// as wide as before.
	Confine func([]string) ([]string, bool)
	// ImageDir is where a tool's pictures are kept — the daemon's data directory, beside the
	// sessions. Supplied by the binary that knows the platform paths; empty means this host keeps
	// no pictures, and a server that returns one is told so in the result rather than having the
	// bytes written somewhere that will not be there tomorrow.
	ImageDir string

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
	argv := append([]string{command}, args...)
	if m.Confine != nil {
		if wrapped, ok := m.Confine(argv); ok {
			argv = wrapped
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
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

// mcpRegisterTimeout bounds the handshake + tool discovery so a hung/misbehaving
// MCP server can't block startup forever (callers pass context.Background()).
const mcpRegisterTimeout = 30 * time.Second

// registerClient is the common logic for registering a client (stdio or HTTP).
func (m *Manager) registerClient(ctx context.Context, name string, client *Client, cmd *exec.Cmd) error {
	// One name, one server. The map used to take the second silently: the first connection stayed
	// out of it — never closed, its subprocess held for the life of the daemon — while its tools
	// stayed registered under names the second now also claimed, and Remove(name) then unregistered
	// only half of them. Unreachable while every name came from a config map, which cannot repeat a
	// key; reachable the moment companions began attaching under names nobody typed.
	m.mu.Lock()
	_, taken := m.servers[name]
	m.mu.Unlock()
	if taken {
		client.Close()
		return fmt.Errorf("mcp: %q is already attached; two servers cannot share one name", name)
	}
	ctx, cancel := context.WithTimeout(ctx, mcpRegisterTimeout)
	defer cancel()
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
		reg := namespacedToolName(name, d.Name)
		t := &mcpTool{client: client, name: reg, remote: d.Name, description: d.Description,
			schema: schema, imageDir: m.ImageDir}
		m.sink.Register(t)
		sc.tools = append(sc.tools, reg)
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

// namespacedToolName builds the registry name for a server's tool as "mcp__<server>__<tool>", each
// part sanitized to the function-name charset ([A-Za-z0-9_-], others → '_'). Namespacing is what
// keeps an MCP tool from SHADOWING a builtin (the registry replaces by name, so a server advertising
// `read`/`write`/`list` would otherwise clobber those tools) or colliding with another server's
// identically-named tool. The server-side name is preserved separately (mcpTool.remote) for the call.
func namespacedToolName(server, tool string) string {
	return "mcp__" + sanitizeToolPart(server) + "__" + sanitizeToolPart(tool)
}

func sanitizeToolPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	return b.String()
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
