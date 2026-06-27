package lua

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// loadOut loads a plugin and returns whatever it logged via magi.log, so a
// test plugin can report its result through the host log.
func loadOut(t *testing.T, manifest, initLua string) (string, error) {
	t.Helper()
	var logged strings.Builder
	h := NewHostWithConfig(HostConfig{
		ToolSink: nil,
		Runtime:  RuntimeInfo{Workdir: t.TempDir()},
		Logf:     func(s string) { logged.WriteString(s + "\n") },
	})
	dir := writePlugin(t, manifest, initLua)
	_, err := h.Load(context.Background(), dir)
	return logged.String(), err
}

// exec without the exec:<cmd> grant is denied at the bridge.
func TestBridgeExecDenied(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`capabilities=["tool"]`,
		`local r, e = magi.exec("echo", {"hi"})
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: exec:echo") {
		t.Errorf("exec should be denied without grant, got: %q", out)
	}
}

// exec WITH the grant runs the command and returns stdout/code.
func TestBridgeExecAllowed(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:echo"]`,
		`local r = magi.exec("echo", {"hello-from-exec"})
magi.log("out=" .. r.stdout .. " code=" .. tostring(r.code))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "hello-from-exec") || !strings.Contains(out, "code=0") {
		t.Errorf("exec should return stdout and code, got: %q", out)
	}
}

// http without net:<host> is denied.
func TestBridgeHTTPDenied(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`capabilities=["tool"]`,
		`local r, e = magi.http{ url = "http://example.com/x" }
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: net:example.com") {
		t.Errorf("http should be denied without net grant, got: %q", out)
	}
}

// http WITH net:<host> fetches the body (RAG-over-HTTP / token exchange).
func TestBridgeHTTPAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth") != "tok" {
			http.Error(w, "no auth", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("CONTEXT-CHUNK"))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	host = strings.SplitN(host, ":", 2)[0] // strip port for the net: grant

	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["net:`+host+`"]`,
		`local r = magi.http{ url = "`+srv.URL+`", headers = { ["X-Auth"] = "tok" } }
magi.log("status=" .. tostring(r.status) .. " body=" .. r.body)`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "status=200") || !strings.Contains(out, "CONTEXT-CHUNK") {
		t.Errorf("http should fetch body with headers, got: %q", out)
	}
}

// open_url rejects non-http(s) schemes even with the grant (no file:// coercion).
func TestBridgeOpenURLRejectsScheme(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`permissions=["exec:open-url"]`,
		`local r, e = magi.open_url("file:///etc/passwd")
magi.log("blocked=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "blocked=true") || !strings.Contains(out, "only http/https") {
		t.Errorf("open_url should reject file:// scheme, got: %q", out)
	}
}

// open_url without the grant is denied.
func TestBridgeOpenURLDenied(t *testing.T) {
	out, err := loadOut(t,
		`name="x"`+"\n"+`capabilities=["tool"]`,
		`local r, e = magi.open_url("https://example.com")
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: exec:open-url") {
		t.Errorf("open_url should be denied without grant, got: %q", out)
	}
}
