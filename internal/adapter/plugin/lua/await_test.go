package lua

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// serve's one-shot mode (no handler) without net:listen is denied.
func TestServeOnceDenied(t *testing.T) {
	out, err := loadOut(t,
		`name="cb"`+"\n"+`capabilities=["tool"]`,
		`local r, e = magi.serve{ port = 8799 }
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: net:listen") {
		t.Errorf("serve (one-shot) should be denied without net:listen: %q", out)
	}
}

// With net:listen, serve's one-shot mode (no handler) blocks for the first matching
// request and returns its query/path. A goroutine simulates the browser redirect.
func TestServeOnceReceives(t *testing.T) {
	go func() {
		// Give the listener a moment to bind, then hit it.
		for i := 0; i < 50; i++ {
			time.Sleep(20 * time.Millisecond)
			resp, err := http.Get("http://127.0.0.1:8798/callback?code=abc123&state=xyz")
			if err == nil {
				resp.Body.Close()
				return
			}
		}
	}()
	out, err := loadOut(t,
		`name="cb"`+"\n"+`permissions=["net:listen"]`,
		`local r = magi.serve{ port = 8798, path = "/callback", timeout = 5 }
magi.log("code=" .. tostring(r.query.code) .. " path=" .. r.path)`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "code=abc123") || !strings.Contains(out, "path=/callback") {
		t.Errorf("serve one-shot did not capture the redirect: %q", out)
	}
}

// respond_html switches serve one-shot to the two-request browser-SSO companion flow:
// a GET is answered with the companion page (and the wait continues), and the POST that
// carries the token is captured and returned with its body. A goroutine plays the browser.
func TestServeCompanionFlow(t *testing.T) {
	go func() {
		base := "http://127.0.0.1:8797"
		// 1) The browser first GETs the companion page. It must not end the wait.
		for i := 0; i < 50; i++ {
			time.Sleep(20 * time.Millisecond)
			resp, err := http.Get(base + "/")
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(b), "COMPANION-PAGE") {
				t.Errorf("GET did not receive companion page, got: %q", string(b))
			}
			break
		}
		// 2) The companion page's JS POSTs the token; this is the captured request.
		for i := 0; i < 50; i++ {
			resp, err := http.Post(base+"/jwt", "text/plain", strings.NewReader("tok-xyz"))
			if err == nil {
				resp.Body.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	out, err := loadOut(t,
		`name="cb"`+"\n"+`permissions=["net:listen"]`,
		`local r = magi.serve{ port = 8797, path = "/jwt", timeout = 5,
  respond_html = "<html>COMPANION-PAGE</html>" }
magi.log("method=" .. tostring(r.method) .. " token=" .. tostring(r.body) .. " path=" .. r.path)`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "method=POST") || !strings.Contains(out, "token=tok-xyz") || !strings.Contains(out, "path=/jwt") {
		t.Errorf("companion flow did not capture the POSTed token: %q", out)
	}
}

// The companion (and plain) one-shot wait fails cleanly on timeout when no request
// arrives — the signal a plugin uses to fall back to the login-method menu.
func TestServeCompanionTimesOut(t *testing.T) {
	out, err := loadOut(t,
		`name="cb"`+"\n"+`permissions=["net:listen"]`,
		`local r, e = magi.serve{ port = 8796, path = "/jwt", timeout = 1,
  respond_html = "<html>COMPANION-PAGE</html>" }
magi.log("nil=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "nil=true") || !strings.Contains(out, "timed out") {
		t.Errorf("companion flow should time out cleanly: %q", out)
	}
}
