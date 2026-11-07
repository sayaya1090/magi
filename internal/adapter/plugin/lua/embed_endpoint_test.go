package lua

import (
	"strings"
	"sync"
	"testing"
)

type stubEmbedReg struct {
	mu         sync.Mutex
	url, model string
	calls      int
}

func (s *stubEmbedReg) SetEndpoint(u, m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.url, s.model = u, m
	s.calls++
}

func TestSetEmbedEndpointWithGrant(t *testing.T) {
	reg := &stubEmbedReg{}
	_, out := loadHost(t, HostConfig{EmbedReg: reg},
		`name="e"`+"\n"+`permissions=["net:127.0.0.1"]`,
		`local r, e = magi.set_embed_endpoint("http://127.0.0.1:11436/v1", "bge-m3")
magi.log("ok=" .. tostring(r == true) .. " err=" .. tostring(e))`,
	)
	if !strings.Contains(out, "ok=true") {
		t.Fatalf("granted endpoint should be set: %q", out)
	}
	if reg.url != "http://127.0.0.1:11436/v1" || reg.model != "bge-m3" {
		t.Fatalf("registry got %q %q", reg.url, reg.model)
	}
}

func TestSetEmbedEndpointDeniedWithoutGrant(t *testing.T) {
	reg := &stubEmbedReg{}
	_, out := loadHost(t, HostConfig{EmbedReg: reg},
		`name="e"`+"\n"+`permissions=["net:127.0.0.1"]`,
		`local r, e = magi.set_embed_endpoint("https://embed.example.com/v1", "m")
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
	)
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: net:embed.example.com") {
		t.Fatalf("ungranted host should be denied: %q", out)
	}
	if reg.calls != 0 {
		t.Fatal("a denied call must not reach the registry")
	}
}

func TestSetEmbedEndpointRejectsOtherSchemesAndClears(t *testing.T) {
	reg := &stubEmbedReg{url: "x"}
	_, out := loadHost(t, HostConfig{EmbedReg: reg},
		`name="e"`+"\n"+`permissions=["net:127.0.0.1"]`,
		`local r, e = magi.set_embed_endpoint("file:///etc/passwd")
magi.log("bad=" .. tostring(r == nil) .. " err=" .. tostring(e))
local r2 = magi.set_embed_endpoint("")
magi.log("clear=" .. tostring(r2 == true))`,
	)
	if !strings.Contains(out, "bad=true") || !strings.Contains(out, "only http/https") {
		t.Fatalf("a non-http scheme should be rejected: %q", out)
	}
	if !strings.Contains(out, "clear=true") || reg.url != "" || reg.calls != 1 {
		t.Fatalf("an empty url should clear the override: out=%q url=%q calls=%d", out, reg.url, reg.calls)
	}
}
