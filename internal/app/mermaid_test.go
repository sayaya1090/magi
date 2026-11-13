package app

import (
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"strings"
	"testing"
)

func TestMermaidHiddenFromAttachedTerminal(t *testing.T) {
	a := New(nil, nil, builtin.Default(), nil, nil, Config{})
	a.mu.Lock()
	a.stateLocked("s").terminalDiagram = true
	a.mu.Unlock()
	for _, s := range a.toolSpecs("s", AgentSpec{}) {
		if s.Name == "mermaid" {
			t.Fatal("terminal offered inline diagram")
		}
	}
}
func TestVisualExplanationSkillAvailableWithoutInstallation(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, Config{})
	b, ok := a.skillBody(t.TempDir(), "visual-explanation")
	if !ok || !strings.Contains(b, "sequenceDiagram") {
		t.Fatal("built-in diagram guide missing")
	}
}
