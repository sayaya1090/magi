package lua

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// This file holds the *gated* host capabilities — running a command, opening a
// URL, and making HTTP requests — each enforced against the plugin's declared
// permissions (exec:<cmd>, net:<host>). They exist so a plugin can drive an
// auth flow (browser SSO + token exchange) or a RAG provider can fetch context
// over HTTP, without widening the default sandbox: a plugin that doesn't declare
// the permission is denied at this layer.

const (
	execTimeout    = 60 * time.Second
	httpTimeout    = 30 * time.Second
	httpBodyMaxLen = 5 << 20 // 5 MiB cap on a fetched body
	execOutputMax  = 1 << 20 // 1 MiB cap on captured output
	serveBodyMax   = 5 << 20 // 5 MiB cap on a request body handed to a serve handler
)

// magi.exec(cmd, args?) -> {stdout=, stderr=, code=} | (nil, err)
// Requires permission "exec:<cmd>". The command is run directly (no shell, so no
// injection), in the workdir, with a bounded timeout.
func (p *plugin) bridgeExec(L *lua.LState) int {
	cmd := L.CheckString(1)
	if !p.perms.allowExec(cmd) {
		return fail(L, "permission denied: exec:"+cmd)
	}
	var args []string
	if tbl, ok := L.Get(2).(*lua.LTable); ok {
		tbl.ForEach(func(_, v lua.LValue) {
			if s, ok := v.(lua.LString); ok {
				args = append(args, string(s))
			}
		})
	}

	to := execTimeout
	if p.execTO > 0 {
		to = p.execTO // the manifest asked for more (or less), within loadPlugin's clamp
	}
	// A per-call timeout, third argument: magi.exec(cmd, args, {timeout="15s"}). Only ever
	// SHORTER than the plugin's bound — the manifest is where a longer need is declared, in the
	// file an auditor reads — and it exists because one plugin's calls are not one kind of call:
	// the same backend plugin that needs five minutes for a model turn must not spend them
	// blocked on a metadata fetch during LOAD, which is magi's own startup.
	if opts, ok := L.Get(3).(*lua.LTable); ok {
		if raw, ok := opts.RawGetString("timeout").(lua.LString); ok {
			if d, err := time.ParseDuration(string(raw)); err == nil && d > 0 && d < to {
				to = d
			}
		}
	}
	// magi.exec(cmd, args, {neutral_dir=true}) runs the command somewhere with nothing in it.
	//
	// A CLI that is being used as a LANGUAGE MODEL should not be reading the workspace, and one of
	// them charges for it whether or not it reads: `claude` walks up from its working directory
	// looking for project configuration and puts what it finds in every request. Measured on this
	// machine with every tool already denied — the same prompt, only the directory changing:
	//
	//	inside the magi repo (7 files under .claude/skills) .... 13,676 billed input tokens
	//	a directory outside it .................................. 2,094
	//
	// So 11,582 tokens per call bought a copy of skills magi had already put in the prompt itself.
	// The walk stops at the directory it is given, which is why an empty one anywhere outside the
	// workspace is the whole fix — measured identical (2,094) in /tmp and under the data dir.
	//
	// A BOOLEAN rather than a path. A plugin naming its own working directory could point at any
	// directory on the machine and have a subprocess read it; this only ever narrows, and the
	// directory it narrows to is the host's, not the plugin's. It lives under the data dir rather
	// than /tmp so it is not a well-known path another user of the machine could pre-create as a
	// symlink to a directory holding a CLAUDE.md — which would be a way to put text in front of a
	// model that nobody in this process chose.
	neutral := false
	if opts, ok := L.Get(3).(*lua.LTable); ok {
		neutral = lua.LVAsBool(opts.RawGetString("neutral_dir"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()
	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = p.dir
	if p.host != nil && p.host.runtime.Workdir != "" {
		c.Dir = p.host.runtime.Workdir
	}
	if neutral {
		if dir, err := p.neutralDir(); err == nil {
			c.Dir = dir
		}
		// A directory that could not be made leaves c.Dir as it was: the call still runs, in the
		// workspace, at the old price. Refusing the turn over a cost saving would trade a backend
		// for a discount.
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &cappedWriter{buf: &stdout, max: execOutputMax}
	c.Stderr = &cappedWriter{buf: &stderr, max: execOutputMax}
	err := c.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return fail(L, "exec: "+err.Error())
		}
	}

	res := L.NewTable()
	L.SetField(res, "stdout", lua.LString(stdout.String()))
	L.SetField(res, "stderr", lua.LString(stderr.String()))
	L.SetField(res, "code", lua.LNumber(code))
	L.Push(res)
	return 1
}

// magi.open_url(url) -> true | (nil, err)
// Opens url in the OS default browser. Requires permission "exec:open-url" and
// an http/https scheme (so the opener can't be coerced into file:// or a
// command-like argument).
func (p *plugin) bridgeOpenURL(L *lua.LState) int {
	raw := L.CheckString(1)
	if !p.perms.allowExec("open-url") {
		return fail(L, "permission denied: exec:open-url")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fail(L, "open_url: only http/https URLs are allowed")
	}

	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", u.String())
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", u.String())
	default:
		c = exec.Command("xdg-open", u.String())
	}
	if err := c.Start(); err != nil {
		return fail(L, "open_url: "+err.Error())
	}
	go c.Wait() // reap without blocking the plugin
	L.Push(lua.LTrue)
	return 1
}

// magi.http{url=, method=, headers={}, body=} -> {status=, body=} | (nil, err)
// Requires permission "net:<host>" for the URL's host. http/https only.
func (p *plugin) bridgeHTTP(L *lua.LState) int {
	spec := L.CheckTable(1)
	raw := spec.RawGetString("url").String()
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fail(L, "http: only http/https URLs are allowed")
	}
	if !p.perms.allowNet(u.Hostname()) {
		return fail(L, "permission denied: net:"+u.Hostname())
	}

	method := "GET"
	if m := spec.RawGetString("method").String(); m != "" {
		method = m
	}
	var body io.Reader
	if b := spec.RawGetString("body"); b != lua.LNil {
		body = bytes.NewReader([]byte(b.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return fail(L, "http: "+err.Error())
	}
	if hv, ok := spec.RawGetString("headers").(*lua.LTable); ok {
		hv.ForEach(func(k, v lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				if vs, ok := v.(lua.LString); ok {
					req.Header.Set(string(ks), string(vs))
				}
			}
		})
	}

	// A redirect must land on a host the plugin is ALSO allowed to reach. Go's default client
	// follows up to 10 redirects and never re-runs the caller's check, so an allow-listed endpoint
	// (or an open redirect on it) could bounce the request to 169.254.169.254 / localhost / an
	// internal service and return its body — defeating the net allow-list entirely. This client
	// re-checks allowNet on every hop.
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("http: stopped after 10 redirects")
		}
		if !p.perms.allowNet(r.URL.Hostname()) {
			return fmt.Errorf("http: redirect to a host you may not reach: %s", r.URL.Hostname())
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return fail(L, "http: "+err.Error())
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, httpBodyMaxLen))

	res := L.NewTable()
	L.SetField(res, "status", lua.LNumber(resp.StatusCode))
	L.SetField(res, "body", lua.LString(string(data)))
	L.Push(res)
	return 1
}

// captured is one request the one-shot server decided to hand back to Lua, along
// with its (already-read) body. Passed over a channel so the HTTP handler never
// touches the Lua state or the plugin lock — the invariant that keeps serveOnce
// deadlock-free even while fire() holds p.mu across the whole hook.
type captured struct {
	r    *http.Request
	body []byte
}

// serveOnce is magi.serve's one-shot mode (no handler): a loopback HTTP listener that
// blocks until the awaited request arrives (or timeout), returns it, then shuts down —
// the OAuth/SSO redirect target. Binds 127.0.0.1 only.
//
// Two shapes, both blocking (so a startup hook can gate session entry until auth
// completes) and both served by a pure-Go handler that never enters Lua:
//
//   - Plain redirect capture (no respond_html): the first request whose path matches
//     `path` is returned as {query, path}; used when the token rides in the query.
//   - Companion (respond_html set): a two-request browser SSO flow. GET requests get
//     served the respond_html companion page (whose JS POSTs the token back) and the
//     wait continues; the POST to `path` is captured and returned as
//     {method, path, query, body, headers}, where body carries the token.
func (p *plugin) serveOnce(L *lua.LState, spec *lua.LTable, port int) int {
	wantPath := spec.RawGetString("path").String()
	companion, hasCompanion := "", false
	if v := spec.RawGetString("respond_html"); v != lua.LNil {
		companion, hasCompanion = v.String(), true
	}
	timeout := 120 * time.Second
	if hasCompanion {
		timeout = 300 * time.Second // browser SSO round-trips are slower than a bare redirect
	}
	if t := int(lua.LVAsNumber(spec.RawGetString("timeout"))); t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fail(L, "serve: listen: "+err.Error())
	}
	hit := make(chan captured, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Companion mode: serve the companion page on any non-capturing request and
		// keep waiting; capture only the POST that carries the token.
		if hasCompanion && !(r.Method == http.MethodPost && (wantPath == "" || r.URL.Path == wantPath)) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(companion))
			return
		}
		if !hasCompanion && wantPath != "" && r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, serveBodyMax))
		select {
		case hit <- captured{r: r, body: body}:
		default:
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Received — you can close this tab.</body></html>"))
	})}
	go srv.Serve(ln)
	defer srv.Close()

	select {
	case c := <-hit:
		r := c.r
		res := L.NewTable()
		q := L.NewTable()
		for k, vs := range r.URL.Query() {
			if len(vs) > 0 {
				L.SetField(q, k, lua.LString(vs[0]))
			}
		}
		L.SetField(res, "query", q)
		L.SetField(res, "path", lua.LString(r.URL.Path))
		L.SetField(res, "method", lua.LString(r.Method))
		L.SetField(res, "body", lua.LString(string(c.body)))
		h := L.NewTable()
		for k := range r.Header {
			L.SetField(h, k, lua.LString(r.Header.Get(k)))
		}
		L.SetField(res, "headers", h)
		L.Push(res)
		return 1
	case <-time.After(timeout):
		return fail(L, "serve: timed out waiting for a request")
	}
}

// magi.serve has two modes, both binding 127.0.0.1 only and requiring "net:listen":
//
//   - WITH a handler — persistent async server:
//     magi.serve{port=, handler=function(req) return resp end} -> {port=, stop=function()}
//   - WITHOUT a handler — one-shot blocking wait (the OAuth/SSO redirect target):
//     magi.serve{port=, path=, timeout=} -> {query={k=v}, path=}  (blocks until the first
//     matching request, then shuts down). Add respond_html= to switch to the two-request
//     browser-SSO companion flow: GET requests are answered with respond_html (whose JS
//     posts the token back) and the wait continues; the POST to path is captured and
//     returned as {method=, path=, query=, body=, headers=} with body carrying the token.
//     This lets a startup hook block session entry until auth completes (default 300s).
//
// Persistent mode: routes every
// request through the Lua handler IN-PROCESS — no external runtime, so it works inside
// the single static binary on every platform. Requires permission "net:listen".
// port omitted/0 picks a free port, readable from the returned table's `port`.
//
// The handler is called as handler(req) where
//
//	req  = { method=, path=, query={k=v}, headers={k=v}, body= }
//
// and returns either a response table (all fields optional)
//
//	resp = { status=200, headers={k=v}, body="" }
//
// or a bare string (taken as a 200 body). A handler that errors, or returns neither,
// yields HTTP 500. The server is closed when the plugin is unloaded/reloaded or via the
// returned stop(). The handler runs in the plugin's single Lua state (serialized with
// tool calls), so a tool must not make a blocking request to its own server from within
// its own call.
func (p *plugin) bridgeServe(L *lua.LState) int {
	if !p.perms.allowNet("listen") {
		return fail(L, "permission denied: net:listen")
	}
	spec := L.CheckTable(1)
	port := int(lua.LVAsNumber(spec.RawGetString("port")))
	if port < 0 || port > 65535 {
		return fail(L, "serve: 'port' must be 0..65535")
	}
	hv := spec.RawGetString("handler")
	if hv == lua.LNil {
		// No handler → one-shot blocking mode (wait for the first request, return it).
		return p.serveOnce(L, spec, port)
	}
	handler, ok := hv.(*lua.LFunction)
	if !ok {
		return fail(L, "serve: 'handler' must be a function (omit it for one-shot mode)")
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fail(L, "serve: listen: "+err.Error())
	}
	actual := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, serveBodyMax))
		status, respBody, respHeaders, ok := p.callServeHandler(handler, r, body)
		if !ok {
			http.Error(w, "plugin handler error", http.StatusInternalServerError)
			return
		}
		switch {
		case status == 0:
			status = http.StatusOK
		case status < 100 || status > 599:
			// An out-of-range code would panic net/http's WriteHeader; reply a clean 500.
			http.Error(w, "plugin handler returned invalid status", http.StatusInternalServerError)
			return
		}
		for k, v := range respHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	})}
	go srv.Serve(ln)
	// p.mu is held during a bridge call (tool Execute / fire), so appending here is safe.
	p.servers = append(p.servers, srv)

	res := L.NewTable()
	L.SetField(res, "port", lua.LNumber(actual))
	L.SetField(res, "stop", L.NewFunction(func(L *lua.LState) int {
		_ = srv.Close() // idempotent; also closed on unload
		return 0
	}))
	L.Push(res)
	return 1
}

// callServeHandler invokes a magi.serve handler under the plugin lock (the Lua
// state is not concurrency-safe) and maps its return to an HTTP response. ok=false
// on any error so the HTTP layer can reply 500.
func (p *plugin) callServeHandler(fn *lua.LFunction, r *http.Request, body []byte) (status int, respBody string, headers map[string]string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	L := p.L
	if L == nil {
		return 0, "", nil, false // plugin unloaded
	}

	req := L.NewTable()
	L.SetField(req, "method", lua.LString(r.Method))
	L.SetField(req, "path", lua.LString(r.URL.Path))
	L.SetField(req, "body", lua.LString(string(body)))
	q := L.NewTable()
	for k, vs := range r.URL.Query() {
		if len(vs) > 0 {
			L.SetField(q, k, lua.LString(vs[0]))
		}
	}
	L.SetField(req, "query", q)
	h := L.NewTable()
	for k := range r.Header {
		L.SetField(h, k, lua.LString(r.Header.Get(k)))
	}
	L.SetField(req, "headers", h)

	if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, req); err != nil {
		p.logf(fmt.Sprintf("[%s] serve handler error: %v", p.name, err))
		return 0, "", nil, false
	}
	result := L.Get(-1)
	L.Pop(1)

	switch v := result.(type) {
	case *lua.LTable:
		out := map[string]string{}
		if ht, ok := v.RawGetString("headers").(*lua.LTable); ok {
			ht.ForEach(func(k, val lua.LValue) {
				if ks, ok := k.(lua.LString); ok {
					if vs, ok := val.(lua.LString); ok {
						out[string(ks)] = string(vs)
					}
				}
			})
		}
		rb := ""
		if b := v.RawGetString("body"); b != lua.LNil {
			rb = b.String()
		}
		return int(lua.LVAsNumber(v.RawGetString("status"))), rb, out, true
	case lua.LString:
		return http.StatusOK, string(v), nil, true
	default:
		p.logf(fmt.Sprintf("[%s] serve handler returned non-table/string", p.name))
		return 0, "", nil, false
	}
}

// magi.set_base_url(url) -> true | (nil, err)
// Redirects the agent's LLM backend to url at runtime — e.g. a loopback server the plugin
// runs via magi.serve, or a corporate gateway whose URL the plugin discovers at login.
// An empty string clears the override and restores the configured backend.
//
// Requires "net:<host>" for the target host. SECURITY: the agent attaches its real API key
// and sends every prompt/response to base(), so granting net:<host> to a plugin authorizes
// it to redirect that credentialed traffic there — grant the host explicitly and minimally.
// The override is cleared automatically when the plugin is unloaded.
func (p *plugin) bridgeSetBaseURL(L *lua.LState) int {
	if p.host == nil || p.host.baseReg == nil {
		return fail(L, "set_base_url: base URL registry not available")
	}
	raw := L.CheckString(1)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fail(L, "set_base_url: only http/https URLs are allowed")
		}
		if !p.perms.allowNet(u.Hostname()) {
			return fail(L, "permission denied: net:"+u.Hostname())
		}
	}
	p.baseTok = p.host.baseReg.SetBaseURL(raw) // token used to release only our own override
	p.baseSet = raw != ""
	p.logf("[" + p.name + "] set LLM base URL: " + raw)
	L.Push(lua.LTrue)
	return 1
}

// cappedWriter discards bytes past max so a runaway command can't exhaust memory.
type cappedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *cappedWriter) Write(b []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		if len(b) > room {
			w.buf.Write(b[:room])
		} else {
			w.buf.Write(b)
		}
	}
	return len(b), nil // report full consumption so the command isn't blocked
}

// neutralDir is the empty directory neutral_dir runs in: one per plugin, under the host's data
// dir, created on demand and never written to by magi. Per plugin rather than shared so one
// backend's CLI cannot leave a file that lands in another's context.
func (p *plugin) neutralDir() (string, error) {
	base := ""
	if p.host != nil {
		base = p.host.dataDir
	}
	if base == "" {
		return "", errors.New("no data dir to put a neutral directory in")
	}
	dir := filepath.Join(base, "neutral", p.name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
