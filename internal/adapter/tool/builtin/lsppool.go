package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A persistent, in-process pool of warm language servers. magi is one long-lived
// process, so instead of a separate socket daemon we keep each server warm here,
// keyed by (workdir, server binary): the cold start — spawn + initialize — is paid
// once, then every later diagnose on that project/language reuses the running
// server via didOpen/didChange. This backs both the manual lsp_diagnostics tool
// and the automatic post-edit diagnostics hook, where cold-starting per edit would
// be untenable. A dead server is transparently restarted; idle ones are reaped;
// all are killed on process exit (CloseLSPPool).

// Test seams: overridable so the pool logic can be driven by an in-process fake
// server (see lsppool_test.go) without a real language-server binary on PATH.
var (
	lspLookPath = exec.LookPath
	// lspSpawn starts a server for the warm pool. It uses context.Background(),
	// NOT a per-call context, so the warm process outlives the call that started
	// it; the pool kills it explicitly (close / reaper / CloseLSPPool).
	lspSpawn = func(srv lspServer, workdir string) (*lspClient, error) {
		return startLSP(context.Background(), srv, workdir)
	}
)

var errLSPDead = errors.New("language server connection closed")

const lspIdleTTL = 3 * time.Minute // reap a warm server unused for this long

// normURI reduces a file URI to a comparison key tolerant of the trailing-slash /
// empty-host forms different servers emit (matches sameURI's normalization).
func normURI(u string) string {
	u = strings.TrimPrefix(u, "file://")
	return strings.TrimPrefix(u, "/")
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// warmLSP is one long-lived server connection. A single reader goroutine owns the
// read side: it routes responses to per-request waiters and publishDiagnostics
// into a per-URI latest map (with a freshness counter so a diagnose call waits for
// a publish that post-dates its own didChange). All writes funnel through send()
// under writeMu because requests, notifications, and the reader's null replies to
// server→client requests can race on the shared stdin pipe.
type warmLSP struct {
	cli    *lspClient
	langID string

	callMu sync.Mutex // serializes one diagnose (didOpen/didChange + wait) at a time

	writeMu sync.Mutex // serializes stdin writes

	mu       sync.Mutex
	latest   map[string][]lspDiagnostic // normURI -> last published diagnostics
	diagSeq  map[string]int             // normURI -> publish counter (freshness)
	opened   map[string]int             // normURI -> last version sent
	waiters  map[int]chan rpcResult     // request id -> response waiter
	reqID    int
	dead     bool
	lastUsed time.Time
	updated  chan struct{} // pinged (non-blocking) on each publish, to wake a waiter
}

func (w *warmLSP) send(m map[string]any) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return w.cli.writeMsg(m)
}

func (w *warmLSP) ping() {
	select {
	case w.updated <- struct{}{}:
	default:
	}
}

// reader is the sole consumer of the server's stdout for this connection's life.
func (w *warmLSP) reader() {
	for {
		m, err := w.cli.readMsg()
		if err != nil {
			w.mu.Lock()
			w.dead = true
			for id, ch := range w.waiters {
				ch <- rpcResult{nil, err}
				delete(w.waiters, id)
			}
			w.mu.Unlock()
			w.ping()
			return
		}
		idRaw, hasID := m["id"]
		method, hasMethod := m["method"]
		switch {
		case hasID && hasMethod: // server→client request — reply null so it doesn't block
			_ = w.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(idRaw), "result": nil})
		case hasID: // response to one of our requests
			var mid int
			if json.Unmarshal(idRaw, &mid) == nil {
				w.mu.Lock()
				ch, ok := w.waiters[mid]
				if ok {
					delete(w.waiters, mid)
				}
				w.mu.Unlock()
				if ok {
					if e, ok := m["error"]; ok {
						ch <- rpcResult{nil, fmt.Errorf("lsp error: %s", string(e))}
					} else {
						ch <- rpcResult{m["result"], nil}
					}
				}
			}
		case hasMethod: // a notification
			var meth string
			if json.Unmarshal(method, &meth) == nil && meth == "textDocument/publishDiagnostics" {
				var p publishDiagParams
				if json.Unmarshal(m["params"], &p) == nil {
					nu := normURI(p.URI)
					w.mu.Lock()
					w.latest[nu] = p.Diagnostics
					w.diagSeq[nu]++
					w.mu.Unlock()
					w.ping()
				}
			}
		}
	}
}

// request sends an RPC and waits for its response (or ctx/death). Server→client
// requests and notifications arriving meanwhile are handled by reader.
func (w *warmLSP) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	w.mu.Lock()
	if w.dead {
		w.mu.Unlock()
		return nil, errLSPDead
	}
	w.reqID++
	id := w.reqID
	ch := make(chan rpcResult, 1)
	w.waiters[id] = ch
	w.mu.Unlock()

	if err := w.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		w.mu.Lock()
		delete(w.waiters, id)
		w.mu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		w.mu.Lock()
		delete(w.waiters, id)
		w.mu.Unlock()
		return nil, ctx.Err()
	}
}

// diagnose opens (first visit) or updates (revisit) absPath on the warm server and
// returns the diagnostics it publishes for that file after the change.
func (w *warmLSP) diagnose(ctx context.Context, absPath string) ([]lspDiagnostic, error) {
	uri := "file://" + absPath
	nu := normURI(uri)
	data, _ := os.ReadFile(absPath)

	w.mu.Lock()
	if w.dead {
		w.mu.Unlock()
		return nil, errLSPDead
	}
	ver := w.opened[nu]
	since := w.diagSeq[nu]
	w.opened[nu] = ver + 1
	w.lastUsed = time.Now()
	w.mu.Unlock()

	var err error
	if ver == 0 {
		err = w.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
			"textDocument": map[string]any{"uri": uri, "languageId": w.langID, "version": 1, "text": string(data)},
		}})
	} else {
		// Full-document sync: send the whole new text as one change (servers default
		// to full sync when incremental wasn't negotiated).
		err = w.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": ver + 1},
			"contentChanges": []map[string]any{{"text": string(data)}},
		}})
	}
	if err != nil {
		return nil, err
	}
	return w.pull(ctx, nu, since, 8*time.Second, 800*time.Millisecond), nil
}

// pull waits for diagnostics for nu that post-date `since` (the publish counter at
// change time). A populated set returns immediately; a fresh-but-empty set (clean,
// or not-yet-analyzed) waits a short quiet window for a populated follow-up. Bound
// by `overall`, the caller's ctx, or the connection dying.
func (w *warmLSP) pull(ctx context.Context, nu string, since int, overall, quiet time.Duration) []lspDiagnostic {
	overallT := time.After(overall)
	var quietT <-chan time.Time
	for {
		w.mu.Lock()
		d := w.latest[nu]
		seq := w.diagSeq[nu]
		dead := w.dead
		w.mu.Unlock()
		if dead {
			return d
		}
		if seq > since {
			if len(d) > 0 {
				return d // fresh, populated — done
			}
			if quietT == nil {
				quietT = time.After(quiet) // fresh but empty: brief wait for a follow-up
			}
			since = seq
		}
		select {
		case <-ctx.Done():
			return d
		case <-overallT:
			return d
		case <-quietT:
			return d
		case <-w.updated:
		}
	}
}

func (w *warmLSP) close() {
	w.mu.Lock()
	w.dead = true
	w.mu.Unlock()
	w.cli.close() // kills the process; reader unblocks on read error and exits
}

// lspPoolManager is the process-global registry of warm servers, mirroring the
// bgManager pattern (servers are OS-global resources, so the pool lives here).
type lspPoolManager struct {
	mu         sync.Mutex
	warm       map[string]*warmLSP
	advised    map[string]bool // (workdir,server) already given install advice this session
	reaperOnce sync.Once
}

var lspPool = &lspPoolManager{warm: map[string]*warmLSP{}}

func poolKey(workdir, server string) string { return workdir + "\x00" + server }

// acquire returns a warm server for (workdir, srv), starting one if absent and
// replacing a dead one. Spawning happens outside the pool lock.
func (m *lspPoolManager) acquire(ctx context.Context, srv lspServer, workdir string) (*warmLSP, error) {
	key := poolKey(workdir, srv.argv[0])

	m.mu.Lock()
	if w := m.warm[key]; w != nil {
		w.mu.Lock()
		dead := w.dead
		if !dead {
			w.lastUsed = time.Now()
		}
		w.mu.Unlock()
		if !dead {
			m.mu.Unlock()
			return w, nil
		}
		delete(m.warm, key)
		go w.close()
	}
	m.mu.Unlock()

	nw, err := startWarm(ctx, srv, workdir)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if existing := m.warm[key]; existing != nil { // lost a start race — keep the winner
		m.mu.Unlock()
		go nw.close()
		return existing, nil
	}
	m.warm[key] = nw
	m.mu.Unlock()
	m.startReaper()
	return nw, nil
}

func (m *lspPoolManager) drop(key string, w *warmLSP) {
	m.mu.Lock()
	if m.warm[key] == w {
		delete(m.warm, key)
	}
	m.mu.Unlock()
	go w.close()
}

// Diagnose returns formatted diagnostics for a just-changed file. missingServer is
// the server binary name when it isn't on PATH (diags empty, no error) so the
// caller can offer install advice; err is a real failure (unsupported file, etc.).
func (m *lspPoolManager) Diagnose(ctx context.Context, workdir, absPath string) (diags, missingServer string, err error) {
	srv, ok := serverFor(absPath)
	if !ok {
		return "", "", fmt.Errorf("no LSP server configured for %s", filepath.Ext(absPath))
	}
	if _, e := lspLookPath(srv.argv[0]); e != nil {
		return "", srv.argv[0], nil
	}
	key := poolKey(workdir, srv.argv[0])
	ds, derr := m.runDiagnose(ctx, srv, workdir, absPath, key)
	if errors.Is(derr, errLSPDead) { // server died — self-heal with a fresh one, once
		ds, derr = m.runDiagnose(ctx, srv, workdir, absPath, key)
	}
	if derr != nil {
		return "", "", derr
	}
	return formatDiagnostics(ds, relForWorkdir(workdir, absPath)), "", nil
}

func (m *lspPoolManager) runDiagnose(ctx context.Context, srv lspServer, workdir, absPath, key string) ([]lspDiagnostic, error) {
	w, err := m.acquire(ctx, srv, workdir)
	if err != nil {
		return nil, err
	}
	w.callMu.Lock()
	ds, derr := w.diagnose(ctx, absPath)
	w.callMu.Unlock()
	if derr != nil {
		m.drop(key, w)
	}
	return ds, derr
}

// markAdvised records that install advice for (workdir, server) has been shown and
// reports whether this call is the first — so the auto-hook nags only once per
// session, while the manual tool (which doesn't call this) advises every time.
func (m *lspPoolManager) markAdvised(workdir, server string) bool {
	key := poolKey(workdir, server)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.advised == nil {
		m.advised = map[string]bool{}
	}
	if m.advised[key] {
		return false
	}
	m.advised[key] = true
	return true
}

func (m *lspPoolManager) startReaper() {
	m.reaperOnce.Do(func() {
		go func() {
			t := time.NewTicker(time.Minute)
			defer t.Stop()
			for range t.C {
				now := time.Now()
				m.mu.Lock()
				for k, w := range m.warm {
					w.mu.Lock()
					idle := now.Sub(w.lastUsed)
					w.mu.Unlock()
					if idle > lspIdleTTL {
						delete(m.warm, k)
						go w.close()
					}
				}
				m.mu.Unlock()
			}
		}()
	})
}

func startWarm(ctx context.Context, srv lspServer, workdir string) (*warmLSP, error) {
	cli, err := lspSpawn(srv, workdir)
	if err != nil {
		return nil, err
	}
	w := &warmLSP{
		cli:      cli,
		langID:   srv.langID,
		latest:   map[string][]lspDiagnostic{},
		diagSeq:  map[string]int{},
		opened:   map[string]int{},
		waiters:  map[int]chan rpcResult{},
		updated:  make(chan struct{}, 1),
		lastUsed: time.Now(),
	}
	go w.reader()

	ictx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if _, err := w.request(ictx, "initialize", map[string]any{
		"processId":    nil,
		"rootUri":      "file://" + workdir,
		"capabilities": map[string]any{"textDocument": map[string]any{"publishDiagnostics": map[string]any{}}},
	}); err != nil {
		w.close()
		return nil, err
	}
	_ = w.send(map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
	return w, nil
}

// CloseLSPPool kills every warm server. Call once on process exit (the twin of
// KillBackgroundProcesses).
func CloseLSPPool() {
	lspPool.mu.Lock()
	ws := make([]*warmLSP, 0, len(lspPool.warm))
	for k, w := range lspPool.warm {
		ws = append(ws, w)
		delete(lspPool.warm, k)
	}
	lspPool.mu.Unlock()
	for _, w := range ws {
		w.close()
	}
}

// AutoDiagnose is the post-edit hook entry point: it returns diagnostics for a
// just-saved file, or — the first time a needed server is missing this session —
// an OS-appropriate install hint (including a prerequisite runtime bootstrap when
// that's missing too). Any failure or an unsupported file degrades to "" so the
// edit turn is never blocked or slowed beyond the pool's own bounded wait.
func AutoDiagnose(ctx context.Context, workdir, absPath, goos string) string {
	diags, missing, err := lspPool.Diagnose(ctx, workdir, absPath)
	if missing != "" {
		if !lspPool.markAdvised(workdir, missing) {
			return ""
		}
		return composeInstallAdvice(missing, goos, prereqMissingFor(missing))
	}
	if err != nil {
		return ""
	}
	return diags
}

// prereqMissingFor reports whether a server's prerequisite runtime is absent from
// PATH (so the install advice can prepend its bootstrap).
func prereqMissingFor(server string) bool {
	_, prereq := serverInstall(server, "")
	bin := prereqBinary(prereq)
	if bin == "" {
		return false
	}
	_, err := lspLookPath(bin)
	return err != nil
}

// relForWorkdir renders absPath relative to workdir (slash-separated), falling
// back to absPath when it isn't under workdir.
func relForWorkdir(workdir, absPath string) string {
	if rel, err := filepath.Rel(workdir, absPath); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(absPath)
}
