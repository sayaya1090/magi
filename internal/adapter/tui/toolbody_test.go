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

func TestFoldRendered(t *testing.T) {
	applyTheme(true)
	var lines []string
	for i := 0; i < maxToolBodyLines+10; i++ {
		lines = append(lines, "line")
	}
	// Collapsed: capped + a "+N more" footer.
	collapsed := foldRendered(lines, false)
	if n := strings.Count(collapsed, "\n") + 1; n != maxToolBodyLines+1 { // +1 footer line
		t.Errorf("collapsed line count = %d, want %d", n, maxToolBodyLines+1)
	}
	if !strings.Contains(collapsed, "+10 more lines") {
		t.Errorf("missing fold footer:\n%s", collapsed)
	}
	// Expanded: all lines, plus a collapse hint, nothing hidden.
	expanded := foldRendered(lines, true)
	if !strings.Contains(expanded, "collapse") {
		t.Error("expanded body should offer to collapse")
	}
	if strings.Contains(expanded, "more lines") {
		t.Error("expanded body should not hide lines")
	}
}

func TestToolBodyOverflow(t *testing.T) {
	m := &Model{width: 100}
	// A bash block with many output lines is foldable (clickable to expand).
	big := "exit 0\n" + strings.Repeat("x\n", maxToolBodyLines+5)
	if !m.toolBodyOverflows(block{kind: blockToolCall, name: "bash", done: true, result: big}) {
		t.Error("large bash output should overflow")
	}
	// A short one does not.
	if m.toolBodyOverflows(block{kind: blockToolCall, name: "bash", done: true, result: "exit 0\nhi"}) {
		t.Error("small bash output should not overflow")
	}
	// A still-running block has no body yet.
	if m.toolBodyOverflows(block{kind: blockToolCall, name: "bash", done: false, result: big}) {
		t.Error("a running block should not be foldable")
	}
}

func TestSplitNumberedLine(t *testing.T) {
	g, c, ok := splitNumberedLine("     1\t#include <stdio.h>")
	if !ok || strings.TrimSpace(g) != "1" || c != "#include <stdio.h>" {
		t.Errorf("splitNumberedLine = (%q,%q,%v)", g, c, ok)
	}
	// A non-numbered note line is rejected.
	if _, _, ok := splitNumberedLine("(note: x not found)"); ok {
		t.Error("note line should not parse as numbered")
	}
}

func TestToolBodyReadGrepListGlob(t *testing.T) {
	m := &Model{width: 100, isDark: true}
	applyTheme(true)

	read := m.toolBody(block{kind: blockToolCall, name: "read", done: true,
		args: `{"path":"x.go"}`, result: "     1\tpackage main\n     2\tfunc main() {}"})
	if len(read) != 2 {
		t.Errorf("read body lines = %d, want 2", len(read))
	}

	grep := m.toolBody(block{kind: blockToolCall, name: "grep", done: true,
		result: `["a.go:3:func main()","b.go:7:return"]`})
	if len(grep) != 2 || !strings.Contains(grep[0], "a.go:3") {
		t.Errorf("grep body = %v", grep)
	}

	glob := m.toolBody(block{kind: blockToolCall, name: "glob", done: true,
		result: `["a.go","b.go","c.go"]`})
	if len(glob) != 3 {
		t.Errorf("glob body lines = %d, want 3", len(glob))
	}

	list := m.toolBody(block{kind: blockToolCall, name: "list", done: true,
		result: `[{"name":"dir","isDir":true},{"name":"f.go","isDir":false}]`})
	if len(list) != 2 || !strings.Contains(list[0], "dir/") {
		t.Errorf("list body = %v", list)
	}
}

func TestClipLine(t *testing.T) {
	if got := clipLine(strings.Repeat("a", 100), 10); len([]rune(got)) != 10 {
		t.Errorf("clipLine width = %d, want 10", len([]rune(got)))
	}
	if !strings.HasPrefix(clipLine("\tx", 80), "    ") {
		t.Error("clipLine should expand tabs")
	}
	// CJK: width is a cell budget, not a rune count. 40 Korean chars = 80 cells;
	// clipping to 20 cells must yield a line that measures <= 20 cells (each Hangul
	// syllable is 2 cells) and carries the "…" affordance — not 20 runes (40 cells).
	if got := clipLine(strings.Repeat("가", 40), 20); cellWidth(got) > 20 {
		t.Errorf("clipLine CJK cellWidth = %d, want <= 20 (got %q)", cellWidth(got), got)
	}
	if got := clipLine(strings.Repeat("가", 40), 20); !strings.HasSuffix(got, "…") {
		t.Errorf("clipLine CJK should end with ellipsis, got %q", got)
	}
	// A line already within budget is returned unchanged (no spurious ellipsis).
	if got := clipLine("가나다", 20); got != "가나다" {
		t.Errorf("clipLine short CJK = %q, want unchanged", got)
	}
}

// TestClipLineAmbiguousWide guards the case the whole width.go machinery exists
// for: a terminal that draws East-Asian *ambiguous* runes (·★→—●) — and the
// ellipsis "…" itself — two cells wide. clipLine's guard measures with cellWidth
// but its truncation counts by ansi.StringWidth (narrow), so without the cell-
// aware back-off it over-fills and the clipped line overflows by up to 100%.
func TestClipLineAmbiguousWide(t *testing.T) {
	setAmbiguousWide(true)
	defer setAmbiguousWide(false)

	// Every one of these glyphs is ambiguous: StringWidth 1, cellWidth 2 here.
	for _, s := range []string{
		strings.Repeat("★", 40),
		strings.Repeat("·", 40),
		strings.Repeat("→", 40),
		strings.Repeat("a★b·", 20),
	} {
		for _, w := range []int{2, 3, 4, 5, 8, 10, 20} {
			got := clipLine(s, w)
			if cw := cellWidth(got); cw > w {
				t.Errorf("clipLine(%.6q…, %d) = %q measures %d cells, want <= %d", s, w, got, cw, w)
			}
		}
	}
}
