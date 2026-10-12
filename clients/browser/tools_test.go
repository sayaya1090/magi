package browser

import (
	"github.com/sayaya1090/magi/internal/app"
	"testing"
)

func TestToolCatalogAndRoleBoundary(t *testing.T) {
	tools := Tools()
	if len(tools) != 5 {
		t.Fatal("unexpected tool surface")
	}
	for _, tool := range tools {
		if !app.ClientRoleAllows("browser", "mcp__browser__"+tool.Name) {
			t.Fatal(tool.Name)
		}
		if tool.ReadOnly != (tool.Name == "list_tabs" || tool.Name == "read_page") {
			t.Fatal("mutation mislabeled", tool.Name)
		}
		schema := tool.InputSchema.(map[string]any)
		if schema["additionalProperties"] != false {
			t.Fatal("unexpected arguments permitted")
		}
	}
	if app.ClientRoleAllows("browser", "bash") || app.ClientRoleAllows("browser", "mcp__mail__read") {
		t.Fatal("browser permissions broadened")
	}
}
