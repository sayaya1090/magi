package builtin

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
)

func TestMermaidRejectsExecutableContent(t *testing.T) {
	for _, src := range []string{"flowchart LR\nclick A call foo()", "flowchart LR\nA[<script>bad</script>]", "%%{init: {}}%%\nflowchart LR", "flowchart LR\nA[https://example.com]", strings.Repeat("x", 24001)} {
		b, _ := json.Marshal(map[string]string{"title": "test", "source": src})
		r, e := (Mermaid{}).Execute(context.Background(), b, port.ToolEnv{})
		if e != nil || !r.IsError {
			t.Fatalf("accepted %q", src[:min(50, len(src))])
		}
	}
	r, e := (Mermaid{}).Execute(context.Background(), json.RawMessage(`{"title":"구성","source":"flowchart LR\n A[에이전트] --> B[개발자]"}`), port.ToolEnv{})
	if e != nil || r.IsError {
		t.Fatalf("valid: %+v %v", r, e)
	}
}
func TestMermaidPreviewUnavailableOverSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "remote")
	if DiagramBrowserAvailable(context.Background()) {
		t.Fatal("remote browser advertised")
	}
	r, _ := (Mermaid{Preview: true}).Execute(context.Background(), json.RawMessage(`{"title":"x","source":"flowchart LR\nA-->B"}`), port.ToolEnv{})
	if !r.IsError {
		t.Fatal("remote preview created")
	}
}
