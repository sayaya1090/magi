package lua

import (
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
