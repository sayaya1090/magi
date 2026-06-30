package lua

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
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

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = p.dir
	if p.host != nil && p.host.runtime.Workdir != "" {
		c.Dir = p.host.runtime.Workdir
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

	resp, err := http.DefaultClient.Do(req)
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

// serveOnce is magi.serve's one-shot mode (no handler): a loopback HTTP listener that
// blocks until the first matching request arrives (or timeout), returns its {query, path},
// then shuts down — the OAuth/SSO redirect target. Binds 127.0.0.1 only.
func (p *plugin) serveOnce(L *lua.LState, spec *lua.LTable, port int) int {
	wantPath := spec.RawGetString("path").String()
	timeout := 120 * time.Second
	if t := int(lua.LVAsNumber(spec.RawGetString("timeout"))); t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fail(L, "serve: listen: "+err.Error())
	}
	hit := make(chan *http.Request, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantPath != "" && r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		select {
		case hit <- r:
		default:
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Received — you can close this tab.</body></html>"))
	})}
	go srv.Serve(ln)
	defer srv.Close()

	select {
	case r := <-hit:
		res := L.NewTable()
		q := L.NewTable()
		for k, vs := range r.URL.Query() {
			if len(vs) > 0 {
				L.SetField(q, k, lua.LString(vs[0]))
			}
		}
		L.SetField(res, "query", q)
		L.SetField(res, "path", lua.LString(r.URL.Path))
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
//     matching request, then shuts down)
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
	p.host.baseReg.SetBaseURL(raw)
	p.baseSet = raw != ""
	p.baseURL = raw // remembered so close() only clears if the registry still holds our value
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
