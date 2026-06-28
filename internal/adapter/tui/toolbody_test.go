package tui

import (
	"strings"
	"testing"
)

func TestBashOutputLines(t *testing.T) {
	// The leading "exit N" status line is stripped (it's the head summary).
	got := bashOutputLines("exit 0\nhello\nworld")
	if len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Errorf("bashOutputLines = %v", got)
	}
	// Empty output (only a status line) yields no body.
	if got := bashOutputLines("exit 0\n"); got != nil {
		t.Errorf("empty output should yield nil, got %v", got)
	}
	// A non-zero exit's output is still returned.
	if got := bashOutputLines("exit 1\nboom"); len(got) != 1 || got[0] != "boom" {
		t.Errorf("non-zero exit body = %v", got)
	}
}

func TestFoldLines(t *testing.T) {
	applyTheme(true)
	var lines []string
	for i := 0; i < maxToolBodyLines+10; i++ {
		lines = append(lines, "line")
	}
	// Collapsed: capped + a "+N more" footer.
	collapsed := foldLines(lines, false, 80, styleToolResult)
	if n := strings.Count(collapsed, "\n") + 1; n != maxToolBodyLines+1 { // +1 footer line
		t.Errorf("collapsed line count = %d, want %d", n, maxToolBodyLines+1)
	}
	if !strings.Contains(collapsed, "+10 more lines") {
		t.Errorf("missing fold footer:\n%s", collapsed)
	}
	// Expanded: all lines, plus a collapse hint.
	expanded := foldLines(lines, true, 80, styleToolResult)
	if !strings.Contains(expanded, "collapse") {
		t.Error("expanded body should offer to collapse")
	}
	if strings.Contains(expanded, "more lines") {
		t.Error("expanded body should not hide lines")
	}
}

func TestToolBodyOverflowAndClip(t *testing.T) {
	// A bash block with many output lines is foldable (clickable to expand).
	big := "exit 0\n" + strings.Repeat("x\n", maxToolBodyLines+5)
	if !toolBodyOverflows(block{kind: blockToolCall, name: "bash", done: true, result: big}) {
		t.Error("large bash output should overflow")
	}
	// A short one does not.
	if toolBodyOverflows(block{kind: blockToolCall, name: "bash", done: true, result: "exit 0\nhi"}) {
		t.Error("small bash output should not overflow")
	}
	// clipLine truncates long lines and expands tabs.
	if got := clipLine(strings.Repeat("a", 100), 10); len([]rune(got)) != 10 {
		t.Errorf("clipLine width = %d, want 10", len([]rune(got)))
	}
	if !strings.HasPrefix(clipLine("\tx", 80), "    ") {
		t.Error("clipLine should expand tabs")
	}
}
