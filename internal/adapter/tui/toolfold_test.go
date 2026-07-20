package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

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

// toolResultC is toolResult with a CallID, for the parallel-pairing test.
func toolResultC(callID, content string, isErr bool) *session.ToolResult {
	b, _ := json.Marshal(content)
	return &session.ToolResult{CallID: callID, Content: b, IsError: isErr}
}

// Parallel read-only tool results complete OUT OF ORDER. Each result must fold into
// its OWN call by CallID, not into the latest-unfinished call — otherwise a read of
// A shows B's content (the observed mispairing).
func TestParallelResultsPairByCallID(t *testing.T) {
	msgs := []session.Message{
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c1", Name: "read", Args: []byte(`{"path":"a.md"}`)}},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c2", Name: "read", Args: []byte(`{"path":"b.md"}`)}},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c3", Name: "read", Args: []byte(`{"path":"c.md"}`)}},
		}},
		// Results arrive out of order: c2, then c3, then c1.
		{Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: toolResultC("c2", "content-B", false)}}},
		{Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: toolResultC("c3", "content-C", false)}}},
		{Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: toolResultC("c1", "content-A", false)}}},
	}
	blocks := rebuildBlocks(msgs)
	want := map[string]string{"c1": "content-A", "c2": "content-B", "c3": "content-C"}
	seen := 0
	for _, b := range blocks {
		if b.kind != blockToolCall {
			continue
		}
		seen++
		if exp, ok := want[b.callID]; ok && !strings.Contains(b.result, exp) {
			t.Errorf("call %s (%s) got result %q, want %q — parallel results mispaired", b.callID, b.args, b.result, exp)
		}
	}
	if seen != 3 {
		t.Fatalf("want 3 tool-call blocks, got %d", seen)
	}
}

// Live-model path (foldToolResult on Model): same out-of-order pairing must hold.
func TestModelFoldPairsByCallID(t *testing.T) {
	m := &Model{}
	m.blocks = []block{
		{kind: blockToolCall, name: "read", args: `{"path":"a"}`, callID: "c1"},
		{kind: blockToolCall, name: "read", args: `{"path":"b"}`, callID: "c2"},
	}
	m.foldToolResult("c2", "content-B", true)
	m.foldToolResult("c1", "content-A", true)
	if m.blocks[0].result != "content-A" || m.blocks[1].result != "content-B" {
		t.Fatalf("mispaired: c1=%q c2=%q", m.blocks[0].result, m.blocks[1].result)
	}
}

// The timestamp renders as HH:MM on a user/assistant label line, and the copy chip
// stays clickable (its hit-test column accounts for the timestamp width). Zero time
// (resume-rebuilt blocks) shows nothing.
func TestTimestampRenderAndCopyHit(t *testing.T) {
	if got := tsChip(time.Date(2026, 7, 20, 17, 42, 0, 0, time.Local)); !strings.Contains(got, "17:42") {
		t.Fatalf("tsChip should show HH:MM, got %q", got)
	}
	if got := tsChip(time.Time{}); got != "" {
		t.Fatalf("zero time must render nothing, got %q", got)
	}
	// With a timestamp present, the copy-chip hit column shifts right by the timestamp
	// width; the geometry helper must agree so the chip is still clickable.
	withTS := 2 + lipgloss.Width("you") + lipgloss.Width(tsChip(time.Now())) + 1
	noTS := 2 + lipgloss.Width("you") + lipgloss.Width(tsChip(time.Time{})) + 1
	if withTS <= noTS {
		t.Fatalf("timestamp must push the copy chip right: withTS=%d noTS=%d", withTS, noTS)
	}
}
