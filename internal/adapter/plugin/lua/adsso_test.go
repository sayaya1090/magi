package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// An adsso-style plugin: set_llm_headers registers a dynamic header that reads a token,
// and a magi.on("startup") hook "logs in" to populate it. The token is still empty right
// after Load — the login flow only runs when the host fires "startup". This is exactly why
// a test must call h.FireEvent("startup") (and re-inspect) to exercise the flow, instead of
// checking state immediately after Load. (magi.nonce supplies the unpredictable state the
// sandbox's seeded math.random cannot.)
func TestStartupFlowPopulatesDynamicHeader(t *testing.T) {
	llm := &fakeLLMReg{}
	h := NewHostWithConfig(HostConfig{
		ToolSink: builtin.NewRegistry(),
		LLMReg:   llm,
		Runtime:  RuntimeInfo{Workdir: t.TempDir()},
		Logf:     func(string) {},
	})
	dir := writePlugin(t, `name="adsso"`+"\n"+`capabilities=["tool"]`,
		`local token = ""
magi.set_llm_headers(function() return { Authorization = "Bearer " .. token } end)
magi.on("startup", function() token = "tok-" .. magi.nonce(4) end)`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(llm.fns) != 1 {
		t.Fatalf("expected one header fn registered, got %d", len(llm.fns))
	}

	// Right after Load the startup hook has NOT run: the token is still empty.
	if got := llm.fns[0]()["Authorization"]; got != "Bearer " {
		t.Errorf("before startup the token should be empty, got %q", got)
	}

	// Firing startup runs the login flow; the dynamic header now carries the token.
	h.FireEvent("startup")
	if got := llm.fns[0]()["Authorization"]; !strings.HasPrefix(got, "Bearer tok-") {
		t.Errorf("after startup the header should carry the token, got %q", got)
	}
}
