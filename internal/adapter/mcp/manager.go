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
	// viaDoor: attached at runtime through port.ToolServers, not declared in config. The door may
	// only remove what the door added — see Detach. A reservation (no client yet) is also a
	// serverConn: it holds the name while the handshake runs.
	viaDoor bool
	pending bool
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
	return m.registerClient(ctx, name, client, cmd, false)
}

// AddHTTP connects to an MCP server via HTTP transport (Streamable HTTP),
// performs the handshake, discovers its tools, and registers them.
func (m *Manager) AddHTTP(ctx context.Context, name, url string, headers map[string]string) error {
	client := newHTTPClient(url, headers, nil)
	return m.registerClient(ctx, name, client, nil, false)
}

// AddHTTPDynamic is like AddHTTP but takes a headers function evaluated fresh on
// every request, so the caller can inject runtime values (current time, model,
// auth tokens) that change between requests rather than being frozen at setup.
func (m *Manager) AddHTTPDynamic(ctx context.Context, name, url string, headersFn func() map[string]string) error {
	client := newHTTPClient(url, nil, headersFn)
	return m.registerClient(ctx, name, client, nil, false)
}

// mcpRegisterTimeout bounds the handshake + tool discovery so a hung/misbehaving
// MCP server can't block startup forever (callers pass context.Background()).
const mcpRegisterTimeout = 30 * time.Second

// registerClient is the common logic for registering a client (stdio or HTTP).
func (m *Manager) registerClient(ctx context.Context, name string, client *Client, cmd *exec.Cmd, viaDoor bool) error {
	// One name, one server — and the name is claimed BEFORE the handshake, which takes up to
	// mcpRegisterTimeout. Checking and then releasing the lock let two attaches under one name both
	// pass the check and both succeed: the loser stayed out of the map, never closed, its
	// subprocess held for the life of the daemon, while its tools stayed registered under names the
	// winner also claimed — and Remove(name) then unregistered half of them. Unreachable while
	// every name came from a config map, which cannot repeat a key; reachable the moment companions
	// began attaching under names nobody typed.
	//
	// Keyed by the SANITISED name, because that is what the tools are named after: "ppt.one" and
	// "ppt_one" are two rows in the map and one namespace in the registry, so the second used to
	// take the first's tool names silently and detaching it left the first claiming to be attached
	// with nothing registered.
	key := sanitizeToolPart(name)
	m.mu.Lock()
	if held, taken := m.servers[key]; taken {
		m.mu.Unlock()
		client.Close()
		if held.name != name {
			return fmt.Errorf("mcp: %q collides with %q, which is already attached (both become %q in tool names)",
				name, held.name, key)
		}
		return fmt.Errorf("mcp: %q is already attached; two servers cannot share one name", name)
	}
	m.servers[key] = &serverConn{name: name, pending: true, viaDoor: viaDoor}
	m.mu.Unlock()
	// The reservation is released on every failure below. Holding it would burn the name for the
	// life of the daemon over one server that was not listening — worse than the collision it is
	// there to prevent, because nothing would ever be able to take it.
	ok := false
	defer func() {
		if ok {
			return
		}
		m.mu.Lock()
		if held := m.servers[key]; held != nil && held.pending {
			delete(m.servers, key)
		}
		m.mu.Unlock()
	}()
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

	sc := &serverConn{name: name, client: client, cmd: cmd, viaDoor: viaDoor}
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
	m.servers[key] = sc
	m.mu.Unlock()
	ok = true

	// Unregister tools when the server goes away. This net holds every server, however it was
	// attached: a config server nobody can reach is exactly as dead as a runtime one.
	go func() {
		<-client.Done()
		m.Remove(name)
	}()
	return nil
}

// Attach is the runtime door (port.ToolServers): connect to an HTTP MCP server, register its
// tools, and answer with the names that were registered.
//
// The names are the point of the answer. A caller that gets "ok" has been told the handshake
// worked; a caller that gets mcp__ppt__render, mcp__ppt__open has been told what it can now ask
// for, and a caller that gets an empty list has been told the server answered and offers nothing —
// three different situations that one ack flattens into one.
func (m *Manager) Attach(ctx context.Context, name, url string, headers map[string]string) ([]string, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("mcp: attach needs a name and a url")
	}
	client := newHTTPClient(url, headers, nil)
	if err := m.registerClient(ctx, name, client, nil, true); err != nil {
		return nil, err
	}
	m.mu.Lock()
	sc := m.servers[sanitizeToolPart(name)]
	m.mu.Unlock()
	if sc == nil {
		return nil, fmt.Errorf("mcp: %q attached and then vanished", name)
	}
	out := make([]string, len(sc.tools))
	copy(out, sc.tools)
	return out, nil
}

// Detach removes a server the door attached, and says whether there was one.
//
// Only what the door attached. Attach is runtime-only by design — nothing it does survives a
// restart — and detach has to match, or the door becomes a way to take away a server the operator
// declared in config, with no way to get it back until the daemon is restarted. The lifetime net
// (Remove, on the client's Done) is deliberately NOT narrowed this way: a config server nobody can
// reach still has to be cleaned up.
func (m *Manager) Detach(name string) (bool, error) {
	key := sanitizeToolPart(name)
	m.mu.Lock()
	sc := m.servers[key]
	if sc == nil {
		m.mu.Unlock()
		return false, nil // already clean, which is not an error to a caller reconnecting
	}
	if !sc.viaDoor {
		m.mu.Unlock()
		return false, fmt.Errorf("mcp: %q was declared in this daemon's config; the door removes only what it attached", name)
	}
	delete(m.servers, key) // taken under the same lock that found it: two detaches, one true
	m.mu.Unlock()
	m.retire(sc)
	return true, nil
}

// Remove unregisters a server's tools and stops it. However it was attached.
func (m *Manager) Remove(name string) {
	key := sanitizeToolPart(name)
	m.mu.Lock()
	sc := m.servers[key]
	delete(m.servers, key)
	m.mu.Unlock()
	m.retire(sc)
}

// retire unregisters the tools and closes the client, outside the lock: Unregister reaches into
// another registry's lock, and Close waits on a subprocess.
func (m *Manager) retire(sc *serverConn) {
	if sc == nil {
		return
	}
	for _, t := range sc.tools {
		m.sink.Unregister(t)
	}
	if sc.client != nil {
		sc.client.Close()
	}
}

// Close stops all servers.
func (m *Manager) Close() {
	m.mu.Lock()
	conns := make([]*serverConn, 0, len(m.servers))
	for _, sc := range m.servers {
		conns = append(conns, sc)
	}
	m.servers = map[string]*serverConn{}
	m.mu.Unlock()
	for _, sc := range conns {
		m.retire(sc)
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
