package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

func toolResult(content string, isErr bool) *session.ToolResult {
	b, _ := json.Marshal(content)
	return &session.ToolResult{Content: b, IsError: isErr}
}

// A tool call and its result render as ONE line whose glyph flips ⚙ → ✓, with the
// result summary folded in — no separate result line.
func TestToolResultFoldsIntoCall(t *testing.T) {
	msgs := []session.Message{
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "grep", Args: []byte(`{"pattern":"x"}`)}},
		}},
		{Role: session.RoleTool, Parts: []session.Part{
			{Kind: session.PartToolResult, ToolResult: toolResult("3 matches in a.go", false)},
		}},
	}
	blocks := rebuildBlocks(msgs)

	calls, results := 0, 0
	for _, b := range blocks {
		switch b.kind {
		case blockToolCall:
			calls++
			if !b.done || !b.ok {
				t.Errorf("folded call should be done+ok, got done=%v ok=%v", b.done, b.ok)
			}
			if !strings.Contains(b.result, "3 matches") {
				t.Errorf("call should carry the result, got %q", b.result)
			}
		case blockToolResult:
			results++
		}
	}
	if calls != 1 || results != 0 {
		t.Fatalf("expected 1 folded call and 0 standalone results, got calls=%d results=%d", calls, results)
	}

	// Rendered line: ✓ glyph present, raw ⚙ gone, summary inline.
	m := &Model{roleColor: map[string]int{}}
	applyTheme(true)
	line := m.renderBlock(blocks[0])
	if !strings.Contains(line, "✓") || strings.Contains(line, "⚙") {
		t.Errorf("completed call should show ✓ not ⚙: %q", line)
	}
	if !strings.Contains(line, "grep") || !strings.Contains(line, "3 matches") {
		t.Errorf("line should include tool name + result summary: %q", line)
	}
}

// An error result flips the glyph to ✗.
func TestToolResultErrorGlyph(t *testing.T) {
	msgs := []session.Message{
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "read", Args: []byte(`{"path":"nope"}`)}},
		}},
		{Role: session.RoleTool, Parts: []session.Part{
			{Kind: session.PartToolResult, ToolResult: toolResult("file not found", true)},
		}},
	}
	blocks := rebuildBlocks(msgs)
	if len(blocks) != 1 || blocks[0].kind != blockToolCall || blocks[0].ok {
		t.Fatalf("expected one failed folded call, got %+v", blocks)
	}
	m := &Model{roleColor: map[string]int{}}
	applyTheme(true)
	if line := m.renderBlock(blocks[0]); !strings.Contains(line, "✗") {
		t.Errorf("failed call should show ✗: %q", line)
	}
}
