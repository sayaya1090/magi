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

func TestMermaidDescriptionsTravelWithPortableSource(t *testing.T) {
	raw := json.RawMessage(`{"title":"구성","source":"flowchart LR\nA[Agent] --> B[Developer]","descriptions":{"Agent":"Reads the request.\nPrepares a reply."},"animate":true}`)
	r, err := (Mermaid{}).Execute(context.Background(), raw, port.ToolEnv{})
	if err != nil || r.IsError {
		t.Fatalf("metadata: %+v %v", r, err)
	}
	var text string
	if err := json.Unmarshal(r.Content, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `%% magi-descriptions {"Agent":"Reads the request.\nPrepares a reply."}`) || !strings.Contains(text, "\n%% magi-animate\n```") {
		t.Fatalf("missing portable metadata: %s", text)
	}
}

func TestMermaidDescriptionLimits(t *testing.T) {
	for _, description := range []string{"<script>bad</script>", "https://example.com", "```", strings.Repeat("x", 2001)} {
		raw, err := json.Marshal(map[string]any{"title": "test", "source": "flowchart LR\nA-->B", "descriptions": map[string]string{"A": description}})
		if err != nil {
			t.Fatal(err)
		}
		r, err := (Mermaid{}).Execute(context.Background(), raw, port.ToolEnv{})
		if err != nil || !r.IsError {
			t.Fatalf("accepted invalid description: %+v %v", r, err)
		}
	}
	descriptions := map[string]string{}
	for i := 0; i < 20; i++ {
		descriptions[strings.Repeat("a", i+1)] = strings.Repeat("x", 1500)
	}
	raw, err := json.Marshal(map[string]any{"title": "test", "source": "flowchart LR\nA-->B", "descriptions": descriptions})
	if err != nil {
		t.Fatal(err)
	}
	r, err := (Mermaid{}).Execute(context.Background(), raw, port.ToolEnv{})
	if err != nil || !r.IsError {
		t.Fatalf("accepted oversized combined source: %+v %v", r, err)
	}
}
