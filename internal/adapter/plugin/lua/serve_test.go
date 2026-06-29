package lua

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// loadHost loads a single plugin and returns the host (so the caller can Unload)
// plus everything it logged via magi.log. Logs are guarded because a serve handler
// may log from the HTTP goroutine.
func loadHost(t *testing.T, cfg HostConfig, manifest, initLua string) (*Host, string) {
	t.Helper()
	var mu sync.Mutex
	var logged strings.Builder
	cfg.Logf = func(s string) { mu.Lock(); logged.WriteString(s + "\n"); mu.Unlock() }
	if cfg.Runtime.Workdir == "" {
		cfg.Runtime.Workdir = t.TempDir()
	}
	h := NewHostWithConfig(cfg)
	dir := writePlugin(t, manifest, initLua)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return h, logged.String()
}

// parsePort extracts the "port=N" line a serve test plugin logs after binding.
func parsePort(t *testing.T, logs string) int {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if i := strings.Index(line, "port="); i >= 0 {
			var p int
			if _, err := fmt.Sscanf(line[i:], "port=%d", &p); err == nil && p > 0 {
				return p
			}
		}
	}
	t.Fatalf("no port logged; logs:\n%s", logs)
	return 0
}

// serve without net:listen is denied at the bridge.
func TestServeDenied(t *testing.T) {
	out, err := loadOut(t,
		`name="srv"`+"\n"+`capabilities=["tool"]`,
		`local s, e = magi.serve{ port = 0, handler = function(req) return {body="x"} end }
magi.log("denied=" .. tostring(s == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: net:listen") {
		t.Errorf("serve should be denied without net:listen: %q", out)
	}
}

// A non-function handler is rejected, not started.
func TestServeRejectsBadHandler(t *testing.T) {
	out, err := loadOut(t,
		`name="srv"`+"\n"+`permissions=["net:listen"]`,
		`local s, e = magi.serve{ port = 0, handler = "nope" }
magi.log("err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "'handler' must be a function") {
		t.Errorf("serve should reject a non-function handler: %q", out)
	}
}

// A serve handler routes a real loopback request: it sees the method, path, query
// and body, and its table return drives status/headers/body. A bare-string return
// is taken as a 200 body.
func TestServeRoundtrip(t *testing.T) {
	h, out := loadHost(t, HostConfig{},
		`name="srv"`+"\n"+`permissions=["net:listen"]`,
		`local s = magi.serve{ port = 0, handler = function(req)
  if req.path == "/echo" then
    return { status = 201, headers = { ["X-Tag"] = "t" }, body = req.method .. ":" .. req.query.q .. ":" .. req.body }
  end
  return "bare-string-body"
end }
magi.log("port=" .. tostring(s.port))`,
	)
	defer func() { _ = h.Unload("srv") }()
	port := parsePort(t, out)

	// Table response: status, header, and echoed method/query/body.
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/echo?q=hi", port), "text/plain", strings.NewReader("BODY"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
	if resp.Header.Get("X-Tag") != "t" {
		t.Errorf("missing handler-set header X-Tag")
	}
	if string(body) != "POST:hi:BODY" {
		t.Errorf("body = %q, want POST:hi:BODY", body)
	}

	// Bare-string return → 200 with the string as the body.
	resp2, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/other", port))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != 200 || string(body2) != "bare-string-body" {
		t.Errorf("bare-string response = %d %q, want 200 bare-string-body", resp2.StatusCode, body2)
	}
}

// A handler that errors yields HTTP 500 rather than crashing the server.
func TestServeHandlerErrorIs500(t *testing.T) {
	h, out := loadHost(t, HostConfig{},
		`name="srv"`+"\n"+`permissions=["net:listen"]`,
		`local s = magi.serve{ port = 0, handler = function(req) error("boom") end }
magi.log("port=" .. tostring(s.port))`,
	)
	defer func() { _ = h.Unload("srv") }()
	port := parsePort(t, out)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/x", port))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("handler error should be 500, got %d", resp.StatusCode)
	}
}

// Unloading the plugin closes its loopback server (no leaked listener after reload).
func TestServeClosedOnUnload(t *testing.T) {
	h, out := loadHost(t, HostConfig{},
		`name="srv"`+"\n"+`permissions=["net:listen"]`,
		`local s = magi.serve{ port = 0, handler = function(req) return "up" end }
magi.log("port=" .. tostring(s.port))`,
	)
	port := parsePort(t, out)
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("server should be up before unload: %v", err)
	}
	resp.Body.Close()

	if err := h.Unload("srv"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if resp, err := http.Get(url); err == nil {
		resp.Body.Close()
		t.Error("server should refuse connections after unload")
	}
}

// stubBaseReg records the last base URL a plugin set.
type stubBaseReg struct {
	mu  sync.Mutex
	url string
}

func (s *stubBaseReg) SetBaseURL(u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.url = u
}

func (s *stubBaseReg) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url
}

// set_base_url to a host the plugin didn't grant is denied (outbound redirect of the
// agent's own traffic is gated like magi.http).
func TestSetBaseURLDenied(t *testing.T) {
	reg := &stubBaseReg{}
	_, out := loadHost(t, HostConfig{BaseReg: reg},
		`name="b"`+"\n"+`capabilities=["tool"]`,
		`local r, e = magi.set_base_url("http://evil.example.com/v1")
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: net:evil.example.com") {
		t.Errorf("set_base_url should be denied without net grant: %q", out)
	}
	if reg.get() != "" {
		t.Errorf("denied call must not reach the registry, got %q", reg.get())
	}
}

// set_base_url rejects non-http(s) schemes even with a matching grant.
func TestSetBaseURLRejectsScheme(t *testing.T) {
	reg := &stubBaseReg{}
	_, out := loadHost(t, HostConfig{BaseReg: reg},
		`name="b"`+"\n"+`permissions=["net:127.0.0.1"]`,
		`local r, e = magi.set_base_url("file:///etc/passwd")
magi.log("err=" .. tostring(e))`,
	)
	if !strings.Contains(out, "only http/https") {
		t.Errorf("set_base_url should reject file:// scheme: %q", out)
	}
	if reg.get() != "" {
		t.Errorf("rejected scheme must not reach the registry, got %q", reg.get())
	}
}

// With the grant, set_base_url reaches the registry; an empty string clears it.
func TestSetBaseURLAllowedAndClear(t *testing.T) {
	reg := &stubBaseReg{}
	loadHost(t, HostConfig{BaseReg: reg},
		`name="b"`+"\n"+`permissions=["net:127.0.0.1"]`,
		`magi.set_base_url("http://127.0.0.1:9123/v1")`,
	)
	if reg.get() != "http://127.0.0.1:9123/v1" {
		t.Errorf("registry url = %q, want the set value", reg.get())
	}

	reg2 := &stubBaseReg{url: "stale"}
	loadHost(t, HostConfig{BaseReg: reg2},
		`name="b"`+"\n"+`permissions=["net:127.0.0.1"]`,
		`magi.set_base_url("")`,
	)
	if reg2.get() != "" {
		t.Errorf("empty string should clear the override, got %q", reg2.get())
	}
}

// set_base_url is unavailable when the host wired no registry (graceful, not a panic).
func TestSetBaseURLNoRegistry(t *testing.T) {
	out, err := loadOut(t,
		`name="b"`+"\n"+`permissions=["net:127.0.0.1"]`,
		`local r, e = magi.set_base_url("http://127.0.0.1/v1")
magi.log("err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "registry not available") {
		t.Errorf("set_base_url without a registry should report unavailable: %q", out)
	}
}
