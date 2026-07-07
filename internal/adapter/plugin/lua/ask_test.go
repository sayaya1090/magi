package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/prompt"
)

type stubPrompter struct {
	gotSpec prompt.Spec
	answers map[string]any
	err     error
}

func (s *stubPrompter) Ask(spec prompt.Spec) (map[string]any, error) {
	s.gotSpec = spec
	return s.answers, s.err
}

// magi.ask builds a Spec from the Lua table, calls the host prompter, and
// returns the answers back to Lua.
func TestBridgeAsk(t *testing.T) {
	pr := &stubPrompter{answers: map[string]any{"how": "browser", "ok": true}}
	var logged strings.Builder
	h := NewHostWithConfig(HostConfig{
		ToolSink: builtin.NewRegistry(),
		Prompter: pr,
		Logf:     func(s string) { logged.WriteString(s + "\n") },
	})
	dir := writePlugin(t, `name="ask"`+"\n"+`capabilities=["tool"]`,
		`local a = magi.ask{ title="Auth", fields={
		   { name="how", type="select", options={"browser","paste"} },
		   { name="ok",  type="confirm" },
		 }}
		 magi.log("how="..a.how.." ok="..tostring(a.ok))`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The spec reached the prompter with fields parsed.
	if pr.gotSpec.Title != "Auth" || len(pr.gotSpec.Fields) != 2 {
		t.Fatalf("spec = %+v", pr.gotSpec)
	}
	if pr.gotSpec.Fields[0].Type != "select" || len(pr.gotSpec.Fields[0].Options) != 2 {
		t.Errorf("field0 = %+v", pr.gotSpec.Fields[0])
	}
	// The answers came back to Lua.
	if !strings.Contains(logged.String(), "how=browser ok=true") {
		t.Errorf("ask result not returned to Lua: %q", logged.String())
	}
}

// A field that omits label/default must reach the prompter as "" — not the literal
// "nil" that LNil.String() yields — so prompt.go's empty-label→Name fallback works
// and the row doesn't render "› nil".
func TestBridgeAskOmittedLabelIsEmpty(t *testing.T) {
	pr := &stubPrompter{answers: map[string]any{}}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Prompter: pr, Logf: func(string) {}})
	dir := writePlugin(t, `name="ask"`+"\n"+`capabilities=["tool"]`,
		`magi.ask{ fields={ { name="how", type="select", options={"browser"} } } }`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(pr.gotSpec.Fields) != 1 {
		t.Fatalf("fields = %+v", pr.gotSpec.Fields)
	}
	if got := pr.gotSpec.Fields[0].Label; got != "" {
		t.Errorf("omitted label = %q, want empty (not the literal \"nil\")", got)
	}
	if got := pr.gotSpec.Fields[0].Default; got != "" {
		t.Errorf("omitted default = %q, want empty", got)
	}
}

// Without a prompter (e.g. headless), magi.ask returns an error the plugin can
// handle.
func TestBridgeAskNoPrompter(t *testing.T) {
	var logged strings.Builder
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Logf: func(s string) { logged.WriteString(s + "\n") }})
	dir := writePlugin(t, `name="ask"`+"\n"+`capabilities=["tool"]`,
		`local a, err = magi.ask{ fields={} }
		 magi.log("nil="..tostring(a==nil).." err="..tostring(err))`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(logged.String(), "nil=true") {
		t.Errorf("ask without prompter should return nil+err: %q", logged.String())
	}
}
