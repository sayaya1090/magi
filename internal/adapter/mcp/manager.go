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
	"github.com/sayaya1090/magi/internal/quietconsole"
)

// ToolSink is the subset of a tool registry the manager needs (satisfied by
// *builtin.Registry).
//
// Both calls run under the manager's own lock, so both must be leaves: a map write under the
// registry's mutex, reaching nothing and blocking on nothing. That is what lets the manager change
// the map and the registry as one fact — split apart, a server's cleanup unregisters names its
// replacement had already registered, and the map says attached while the registry has nothing.
type ToolSink interface {
	Register(port.Tool)
	Unregister(name string)
}

// Manager owns running MCP servers and keeps their tools registered in a shared
// sink. When a server exits, its tools are unregistered automatically (F-MCP).
type Manager struct {
	sink ToolSink
	// byName is the tool object registered under each namespaced name. Held so a second attach
	// under the same server name can MERGE its hand into the tool already registered instead of
	// replacing it — the registry is keyed by name and would otherwise drop the first silently.
	byName map[string]*mcpTool
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
	// owner: the conversation these tools belong to, empty for the whole daemon. Held here so a
	// later attach under the same name can see whose it was.
	owner string
	// viaDoor: attached at runtime through port.ToolServers, not declared in config. The door may
	// only remove what the door added — see Detach. A reservation (client still nil) is also a
	// serverConn: it holds the name while the handshake runs, and the SAME object is filled in and
	// published when the handshake wins, so the pointer is this server's identity for its whole
	// life. Everything that removes compares it, because a name outlives the server that held it.
	viaDoor bool
}

// NewManager returns a manager that registers tools into sink.
func NewManager(sink ToolSink) *Manager {
	return &Manager{sink: sink, servers: map[string]*serverConn{}, byName: map[string]*mcpTool{}}
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
	quietconsole.Apply(cmd) // an MCP server is usually a console program; no window for it
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
	_, err = m.registerClient(ctx, name, client, cmd, false, "")
	return err
}

// AddHTTP connects to an MCP server via HTTP transport (Streamable HTTP),
// performs the handshake, discovers its tools, and registers them.
func (m *Manager) AddHTTP(ctx context.Context, name, url string, headers map[string]string) error {
	client := newHTTPClient(url, headers, nil)
	_, err := m.registerClient(ctx, name, client, nil, false, "")
	return err
}

// AddHTTPDynamic is like AddHTTP but takes a headers function evaluated fresh on
// every request, so the caller can inject runtime values (current time, model,
// auth tokens) that change between requests rather than being frozen at setup.
func (m *Manager) AddHTTPDynamic(ctx context.Context, name, url string, headersFn func() map[string]string) error {
	client := newHTTPClient(url, nil, headersFn)
	_, err := m.registerClient(ctx, name, client, nil, false, "")
	return err
}

// mcpRegisterTimeout bounds the handshake + tool discovery so a hung/misbehaving
// MCP server can't block startup forever (callers pass context.Background()).
const mcpRegisterTimeout = 30 * time.Second

// mcpProbeTimeout bounds the is-anybody-home probe of a name's current holder. Short, because a
// live server answers a ping in milliseconds and a dead one refuses the connection immediately;
// the deadline only matters for a hung-but-listening holder, which is treated as alive.
const mcpProbeTimeout = 2 * time.Second

// registerClient is the common logic for registering a client (stdio or HTTP). It answers with the
// published server so a caller can read the tools it registered without going back to the map by
// name — by then the name may be someone else's.
// owner is the conversation these tools belong to; empty means the whole daemon. Config-declared
// servers pass empty, which is the only thing they ever meant.
func (m *Manager) registerClient(ctx context.Context, name string, client *Client, cmd *exec.Cmd, viaDoor bool, owner string) (*serverConn, error) {
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
	key := serverKey(sanitizeToolPart(name), owner)
	var sc *serverConn
	for attempt := 0; ; attempt++ {
		m.mu.Lock()
		held, taken := m.servers[key]
		if !taken {
			sc = &serverConn{name: name, cmd: cmd, viaDoor: viaDoor, owner: owner}
			m.servers[key] = sc
			m.mu.Unlock()
			break
		}
		heldClient, heldDoor, heldName := held.client, held.viaDoor, held.name
		m.mu.Unlock()
		// A held name is not always a live holder. The lifetime net waits on the client's Done,
		// which an HTTP transport never closes on its own — nothing there observes the far side —
		// so a hand that died without detaching (kill -9 took the IDE) held its name for the life
		// of the daemon, and every reconnect was refused for ever. The one moment somebody CARES
		// about the name, the holder is asked directly. Only a fully published holder: a
		// reservation (client still nil) is a handshake in flight, alive by definition. Only
		// transport-level "nobody was there" counts as dead — a refusal, an error reply, or the
		// probe's own deadline all mean somebody is home, so two LIVE holders still cannot share
		// a name. One attempt: the retry after a removal is a fresh claim, and whoever raced in
		// behind the removal is a live claimant this attach must not eat.
		// Only a holder the door itself attached: Detach's law ("the door removes only what it
		// attached") holds for the probe too, or a dead CONFIG-declared server's name is evicted
		// and re-claimed door-owned — and a later mcp-detach then removes what the operator
		// declared, gone until restart. A dead config holder keeps its name; it is the
		// operator's to take back.
		if attempt == 0 && heldDoor && heldClient != nil {
			pctx, pcancel := context.WithTimeout(ctx, mcpProbeTimeout)
			alive := heldClient.Reachable(pctx)
			pcancel()
			if !alive {
				m.removeConn(key, held)
				continue
			}
		}
		client.Close()
		if heldName != name {
			return nil, fmt.Errorf("mcp: %q collides with %q, which is already attached (both become %q in tool names)",
				name, heldName, key)
		}
		return nil, fmt.Errorf("mcp: %q is already attached; two servers cannot share one name", name)
	}
	// The reservation is released on every failure below. Holding it would burn the name for the
	// life of the daemon over one server that was not listening — worse than the collision it is
	// there to prevent, because nothing would ever be able to take it.
	//
	// Released by identity, not by "whatever is under the key and unfinished": a Detach during the
	// handshake frees the name, a second attach may already have reserved it, and deleting that
	// one would hand the name to a third while the second is still handshaking.
	ok := false
	defer func() {
		if ok {
			return
		}
		m.mu.Lock()
		if m.servers[key] == sc {
			delete(m.servers, key)
		}
		m.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, mcpRegisterTimeout)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp: initialize %q: %w", name, err)
	}
	defs, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp: list tools %q: %w", name, err)
	}

	tools := make([]*mcpTool, 0, len(defs))
	for _, d := range defs {
		schema := d.InputSchema
		if len(schema) == 0 {
			schema = []byte(`{"type":"object"}`)
		}
		t := &mcpTool{byOwner: map[string]*Client{owner: client},
			name: namespacedToolName(name, d.Name), remote: d.Name,
			description: d.Description, schema: schema, imageDir: m.ImageDir,
			readOnly: d.Annotations != nil && d.Annotations.ReadOnlyHint}
		tools = append(tools, t)
	}

	m.mu.Lock()
	// Our reservation is our identity. If the key no longer holds it, the name was taken from us
	// while we were handshaking — a Detach for the crash this attach is recovering from, or Close
	// — and the tools under that name belong to whoever holds it now. Publishing here would put a
	// second server in the map under one name and let this one'"'"'s eventual removal unregister the
	// other'"'"'s tools. Fold up and say so instead: a caller told "attached" that then finds nothing
	// registered has been told something it has no way to check.
	if m.servers[key] != sc {
		m.mu.Unlock()
		client.Close()
		return nil, fmt.Errorf("mcp: %q was removed while it was attaching", name)
	}
	// Registering the tools under the same lock that publishes the server is what makes the map
	// and the registry one fact. Split, an unregister running outside the lock deletes names the
	// server that just took the key had already registered — the map says attached and the
	// registry has nothing. Safe because the sink is a leaf: Register is a map write under the
	// registry'"'"'s own mutex and never reaches back here, so Manager.mu → sink has no other
	// direction to meet. Close still happens outside the lock; it waits on a subprocess.
	for _, t := range tools {
		// **덮지 않고 합친다.** 레지스트리는 이름으로 키를 잡으므로, 같은 서버 이름으로 둘째가
		// 붙으면 `Register` 는 첫째의 도구 객체를 통째로 갈아 치운다 — 그러면 첫째 대화는
		// 자기 손을 조용히 잃는다(광고에서도 빠지고, 부르면 남의 손으로 간다).
		//
		// 그래서 이름당 도구는 하나로 두고 **손만 주인별로 더한다.** 주인이 같으면 새것으로
		// 갈아 끼운다 — 그건 재부착이고, 앞의 손은 죽은 것이다.
		if had, ok := m.byName[t.name]; ok && had != nil {
			had.adopt(owner, client)
			sc.tools = append(sc.tools, t.name)
			continue
		}
		m.sink.Register(t)
		m.byName[t.name] = t
		sc.tools = append(sc.tools, t.name)
	}
	sc.client = client
	m.mu.Unlock()
	ok = true

	// Unregister tools when the server goes away. This net holds every server, however it was
	// attached: a config server nobody can reach is exactly as dead as a runtime one. It removes by
	// identity: retiring a dead server closes its client, which wakes this goroutine, and by then
	// the name it was given may belong to the replacement that was attached in its place.
	go m.watch(key, sc)
	return sc, nil
}

// watch is the lifetime net's body, named so a test can fix WHEN it runs: the interesting order is
// a death that arrives after a replacement has taken the name, and a goroutine racing a second
// attach can only be made likely, not certain.
func (m *Manager) watch(key string, sc *serverConn) {
	<-sc.client.Done()
	m.removeConn(key, sc)
}

// Attach is the runtime door (port.ToolServers): connect to an HTTP MCP server, register its
// tools, and answer with the names that were registered.
//
// The names are the point of the answer. A caller that gets "ok" has been told the handshake
// worked; a caller that gets mcp__ppt__render, mcp__ppt__open has been told what it can now ask
// for, and a caller that gets an empty list has been told the server answered and offers nothing —
// three different situations that one ack flattens into one.
// owner names the conversation these tools belong to; empty is the whole daemon — what every
// caller meant before the parameter existed (port.ToolServers).
func (m *Manager) Attach(ctx context.Context, owner, name, url string, headers map[string]string) ([]string, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("mcp: attach needs a name and a url")
	}
	client := newHTTPClient(url, headers, nil)
	sc, err := m.registerClient(ctx, name, client, nil, true, owner)
	if err != nil {
		return nil, err
	}
	// Still the one we published, not merely something under that name. Going back to the map by
	// name would answer with a replacement'"'"'s tool names as if this attach had registered them.
	m.mu.Lock()
	// **주인까지 든 열쇠로 본다.** `registerClient` 는 (이름, 주인)으로 예약하는데 이 검사는 이름만
	// 보고 있었다 — 주인 달린 등록은 매번 「붙었다가 사라졌다」로 거절됐다(2026-09-05 실물, PowerPoint
	// 덱 둘 다). 단위 시험은 `mcpTool` 만 직접 재고 이 문에 주인을 실어 본 적이 없었다.
	ours := m.servers[serverKey(sanitizeToolPart(name), owner)] == sc
	m.mu.Unlock()
	if !ours {
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
// owner is whose registration to remove; empty is the daemon-wide one.
func (m *Manager) Detach(owner, name string) (bool, error) {
	key := serverKey(sanitizeToolPart(name), owner)
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
	m.unpublishLocked(key, sc) // taken under the same lock that found it: two detaches, one true
	m.mu.Unlock()
	closeConn(sc)
	// True even when what we took was a reservation whose handshake is still running: the name is
	// free, and that attach will find its reservation gone and fold up rather than land behind us.
	// Answering false there would tell a caller reconnecting after a crash that nothing was there,
	// moments before the thing it wanted gone finished arriving.
	return true, nil
}

// Remove unregisters a server's tools and stops it. However it was attached — this is the
// operator'"'"'s door, so it takes whatever holds the name. The lifetime net uses removeConn instead,
// because a dead server must not reach past its own death to its successor.
func (m *Manager) Remove(name string) {
	key := sanitizeToolPart(name)
	m.mu.Lock()
	sc := m.servers[key]
	if sc != nil {
		m.unpublishLocked(key, sc)
	}
	m.mu.Unlock()
	closeConn(sc)
}

// removeConn removes sc, and only sc: if the key has moved on to another server, this one is
// already gone and there is nothing to do. Removing by name alone let a dead server'"'"'s cleanup
// reach its own replacement — closing the dead client woke the net goroutine, which deleted
// whatever had taken the name and unregistered the tools the replacement had just registered,
// after Attach had already handed those names to the caller as the answer.
func (m *Manager) removeConn(key string, sc *serverConn) {
	m.mu.Lock()
	if m.servers[key] != sc {
		m.mu.Unlock()
		return
	}
	m.unpublishLocked(key, sc)
	m.mu.Unlock()
	closeConn(sc)
}

// unpublishLocked takes a server out of the map and its tools out of the registry, under the one
// lock, so the two never disagree about who is attached. Caller holds m.mu.
func (m *Manager) unpublishLocked(key string, sc *serverConn) {
	delete(m.servers, key)
	for _, name := range sc.tools {
		// **주인이 여럿이면 내 손만 놓는다.** 통째로 떼면 아직 붙어 있는 다른 대화가 자기 손을
		// 잃고, 그 대화는 아무 잘못도 안 했다.
		if had, ok := m.byName[name]; ok && had != nil {
			if left := had.release(sc.owner); left > 0 {
				continue
			}
			delete(m.byName, name)
		}
		m.sink.Unregister(name)
	}
}

// closeConn stops the client, outside the lock: Close waits on a subprocess. The tools are already
// gone by the time this runs — they are unregistered under the same lock that took the server out.
func closeConn(sc *serverConn) {
	if sc == nil || sc.client == nil {
		return
	}
	sc.client.Close()
}

// Close stops all servers.
func (m *Manager) Close() {
	m.mu.Lock()
	conns := make([]*serverConn, 0, len(m.servers))
	for key, sc := range m.servers {
		conns = append(conns, sc)
		m.unpublishLocked(key, sc)
	}
	m.mu.Unlock()
	for _, sc := range conns {
		closeConn(sc)
	}
}

// serverKey 는 등록 하나의 신원이다. **이름만으로는 모자라다** — 대화 둘이 같은 서버 이름으로
// 붙으면 둘 다 살아야 하고(각자 자기 손), 이름만 키로 쓰면 둘째가 첫째를 밀어낸다.
// 주인이 비면 옛 키 그대로라, 이 변경 전에 붙어 있던 것과 이름이 안 달라진다.
func serverKey(name, owner string) string {
	if owner == "" {
		return name
	}
	return name + "\x00" + owner
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
