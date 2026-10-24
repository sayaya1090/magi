package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestStrictBashRefusesUnconfinedFallbackAndBackground(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	env := port.ToolEnv{Workdir: t.TempDir(), Sandbox: port.SandboxSpec{Mode: "workspace-write", StrictWrites: true}}
	for _, args := range []string{`{"command":"echo must-not-run"}`, `{"command":"echo must-not-run","background":"true"}`} {
		result, err := (Bash{}).Execute(context.Background(), json.RawMessage(args), env)
		if err != nil || !result.IsError || !strings.Contains(string(result.Content), "isolated execution") {
			t.Fatalf("unconfined shell allowed: %s %v", result.Content, err)
		}
	}
}
