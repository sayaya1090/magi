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

// magi.await_callback{port=, path=, timeout=} -> {query={k=v}, path=} | (nil, err)
// Starts a one-shot loopback HTTP listener (the OAuth/SSO redirect target),
// blocks until a matching request arrives (or timeout), then shuts down. Requires
// permission "net:listen". Binds 127.0.0.1 only.
func (p *plugin) bridgeAwaitCallback(L *lua.LState) int {
	if !p.perms.allowNet("listen") {
		return fail(L, "permission denied: net:listen")
	}
	spec := L.CheckTable(1)
	port := int(lua.LVAsNumber(spec.RawGetString("port")))
	if port <= 0 || port > 65535 {
		return fail(L, "await_callback: 'port' must be 1..65535")
	}
	wantPath := spec.RawGetString("path").String()
	timeout := 120 * time.Second
	if t := int(lua.LVAsNumber(spec.RawGetString("timeout"))); t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fail(L, "await_callback: listen: "+err.Error())
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
		_, _ = w.Write([]byte("<html><body>Authentication received — you can close this tab.</body></html>"))
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
		return fail(L, "await_callback: timed out")
	}
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
