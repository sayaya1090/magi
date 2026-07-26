package lua

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// The shipped codemate plugin must load through the real host: manifest present,
// every capability/permission it uses declared, and the Lua compiles and runs its
// top level (set_base_url, header fn, commands, doctor probes).
func TestCodemateLoads(t *testing.T) {
	requireCodematePlugin(t)
	llm := &fakeLLMReg{}
	h := NewHostWithConfig(HostConfig{
		ToolSink: builtin.NewRegistry(),
		LLMReg:   llm,
		Runtime:  RuntimeInfo{Workdir: t.TempDir()},
		Logf:     func(string) {},
	})
	if _, err := h.Load(context.Background(), codematePluginDir); err != nil {
		t.Fatalf("codemate load: %v", err)
	}
	if len(llm.fns) != 1 {
		t.Errorf("expected the llm-headers fn registered, got %d", len(llm.fns))
	}
	if got := len(h.DoctorProbes()); got != 4 {
		t.Errorf("expected 4 doctor probes (token/portal/models/context-window), got %d", got)
	}
}
