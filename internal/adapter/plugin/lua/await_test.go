package lua

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// await_callback without net:listen is denied.
func TestAwaitCallbackDenied(t *testing.T) {
	out, err := loadOut(t,
		`name="cb"`+"\n"+`capabilities=["tool"]`,
		`local r, e = magi.await_callback{ port = 8799 }
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: net:listen") {
		t.Errorf("await_callback should be denied without net:listen: %q", out)
	}
}

// With net:listen, await_callback receives a loopback request and returns its
// query params. A goroutine simulates the browser redirect.
func TestAwaitCallbackReceives(t *testing.T) {
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
		`local r = magi.await_callback{ port = 8798, path = "/callback", timeout = 5 }
magi.log("code=" .. tostring(r.query.code) .. " path=" .. r.path)`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "code=abc123") || !strings.Contains(out, "path=/callback") {
		t.Errorf("await_callback did not capture the redirect: %q", out)
	}
}
