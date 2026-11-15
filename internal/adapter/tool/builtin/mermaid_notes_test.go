package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Element details belong in descriptions (the (i) badge and its hover note), not in prose under
// the diagram: the tool says so where the model reads it, and nothing tells it to explain below.
func TestMermaidSteersElementDetailsIntoDescriptions(t *testing.T) {
	m := Mermaid{}
	if !strings.Contains(m.Description(), "each element's details") {
		t.Error("the tool description does not send element details to descriptions")
	}
	if !strings.Contains(string(m.Schema()), "each element's details") {
		t.Error("the descriptions field does not say what goes in it")
	}
	r, err := m.Execute(context.Background(), json.RawMessage(`{"title":"x","source":"flowchart LR\nA-->B"}`), port.ToolEnv{})
	if err != nil || r.IsError {
		t.Fatalf("%+v %v", r, err)
	}
	var text string
	_ = json.Unmarshal(r.Content, &text)
	if strings.Contains(text, "explain its important relationships") || !strings.Contains(text, "do not repeat") {
		t.Errorf("the tool result still asks for prose about the elements: %s", text[:min(300, len(text))])
	}
}
