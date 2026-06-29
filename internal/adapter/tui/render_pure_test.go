package tui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/session"
)

func lipColor(hex string) color.Color { return lipgloss.Color(hex) }
func width(s string) int              { return lipgloss.Width(s) }

// rgb extracts 8-bit R,G,B from a color.Color (lipgloss.Color is a func in v2, not a
// string type, so compare via RGBA instead of a hex string).
func rgb(c color.Color) (int, int, int) {
	r, g, b, _ := c.RGBA()
	return int(r >> 8), int(g >> 8), int(b >> 8)
}

// balanceFences closes a dangling ``` so streaming code highlights live; an even
// count is left alone.
func TestBalanceFences(t *testing.T) {
	if got := balanceFences("```go\nx := 1"); !strings.HasSuffix(got, "\n```") {
		t.Errorf("odd fence should be closed: %q", got)
	}
	closed := "```go\nx := 1\n```"
	if got := balanceFences(closed); got != closed {
		t.Errorf("even fences must be untouched: %q", got)
	}
	if got := balanceFences("no fences here"); got != "no fences here" {
		t.Errorf("no fence should be untouched: %q", got)
	}
}

// argPath previews a tool call's path arg, or "" when absent/invalid.
func TestArgPath(t *testing.T) {
	if got := argPath(`{"path":"src/main.go","content":"x"}`); got != "path=src/main.go" {
		t.Errorf("argPath = %q, want path=src/main.go", got)
	}
	if got := argPath(`{"command":"ls"}`); got != "" {
		t.Errorf("no path arg should yield empty, got %q", got)
	}
	if got := argPath("not json"); got != "" {
		t.Errorf("invalid JSON should yield empty, got %q", got)
	}
}

// diffBaseLine maps a diff's first line to the new-side file line: 1 for a whole-file
// write, the edit tool's reported " @N" start for an edit, 0 (no gutter) otherwise.
func TestDiffBaseLine(t *testing.T) {
	if got := diffBaseLine(block{name: "write"}); got != 1 {
		t.Errorf("write diff should start at line 1, got %d", got)
	}
	if got := diffBaseLine(block{name: "edit", result: "applied @42"}); got != 42 {
		t.Errorf("edit diff should use the @N start, got %d", got)
	}
	if got := diffBaseLine(block{name: "edit", result: "no marker"}); got != 0 {
		t.Errorf("edit without @N should yield 0, got %d", got)
	}
	if got := diffBaseLine(block{name: "bash"}); got != 0 {
		t.Errorf("non-file tool should yield 0, got %d", got)
	}
}

// userPrompts seeds input history with genuine user prompts only — skipping empty
// turns and injected subagent results (which are also user-role).
func TestUserPrompts(t *testing.T) {
	txt := func(s string) session.Part { return session.Part{Kind: session.PartText, Text: s} }
	msgs := []session.Message{
		{Role: session.RoleUser, Parts: []session.Part{txt("first")}},
		{Role: session.RoleAssistant, Parts: []session.Part{txt("reply")}},          // not user
		{Role: session.RoleUser, Parts: []session.Part{txt("[subagent x] result")}}, // injected
		{Role: session.RoleUser, Parts: []session.Part{txt("   ")}},                 // empty
		{Role: session.RoleUser, Parts: []session.Part{txt("second")}},
	}
	got := userPrompts(msgs)
	want := []string{"first", "second"}
	if len(got) != len(want) || got[0] != "first" || got[1] != "second" {
		t.Errorf("userPrompts = %v, want %v", got, want)
	}
}

// joinTextParts concatenates text parts with newlines and ignores non-text parts.
func TestJoinTextParts(t *testing.T) {
	parts := []session.Part{
		{Kind: session.PartText, Text: "a"},
		{Kind: session.PartToolCall},
		{Kind: session.PartText, Text: "b"},
	}
	if got := joinTextParts(parts); got != "a\nb" {
		t.Errorf("joinTextParts = %q, want \"a\\nb\"", got)
	}
}

// agentSummary collapses duplicate roles as "role×N" while preserving first-seen order.
func TestAgentSummary(t *testing.T) {
	got := agentSummary([]string{"explore", "coder", "explore", "explore"})
	if got != "explore×3, coder" {
		t.Errorf("agentSummary = %q, want \"explore×3, coder\"", got)
	}
	if got := agentSummary(nil); got != "" {
		t.Errorf("empty input should yield empty, got %q", got)
	}
}

// removeFirst drops only the first occurrence; a missing value leaves the slice as-is.
func TestRemoveFirst(t *testing.T) {
	if got := removeFirst([]string{"a", "b", "a"}, "a"); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("removeFirst should drop only the first match, got %v", got)
	}
	if got := removeFirst([]string{"a", "b"}, "z"); len(got) != 2 {
		t.Errorf("missing value should leave the slice unchanged, got %v", got)
	}
}

// permHint gives a distinct one-liner per permission mode (and a default).
func TestPermHint(t *testing.T) {
	for _, mode := range []string{"ask", "auto", "allow", "deny"} {
		if permHint(mode) == "" {
			t.Errorf("permHint(%q) should be non-empty", mode)
		}
	}
	if permHint("allow") == permHint("deny") {
		t.Error("distinct modes should have distinct hints")
	}
	if permHint("weird") != permHint("ask") {
		t.Error("unknown mode should fall back to the ask/default hint")
	}
}

// blendColor returns endpoint a at t<=0, endpoint b at t>=1, and a midpoint between.
func TestBlendColor(t *testing.T) {
	black, white := lipColor("#000000"), lipColor("#FFFFFF")
	if r, g, b := rgb(blendColor(black, white, 0)); r|g|b != 0 {
		t.Errorf("t=0 should be the start color (black), got %d,%d,%d", r, g, b)
	}
	if r, g, b := rgb(blendColor(black, white, 1)); r != 255 || g != 255 || b != 255 {
		t.Errorf("t=1 should be the end color (white), got %d,%d,%d", r, g, b)
	}
	// t>1 is clamped to the end color (not extrapolated past it).
	if r, g, b := rgb(blendColor(black, white, 5)); r != 255 || g != 255 || b != 255 {
		t.Errorf("t>1 should clamp to white, got %d,%d,%d", r, g, b)
	}
	if r, g, b := rgb(blendColor(black, white, 0.5)); r != 127 || g != 127 || b != 127 {
		t.Errorf("t=0.5 black→white should be ~127 grey, got %d,%d,%d", r, g, b)
	}
}

// oneLine collapses internal whitespace/newlines and truncates to a display width.
func TestOneLine(t *testing.T) {
	if got := oneLine("a\n b\t c", 100); got != "a b c" {
		t.Errorf("oneLine should collapse whitespace, got %q", got)
	}
	got := oneLine("abcdefghij", 5)
	if width(got) > 5 || !strings.HasSuffix(got, "…") {
		t.Errorf("oneLine truncation = %q, want width<=5 ending in …", got)
	}
	// Wide CJK runes are 2 cells each — oneLine budgets by DISPLAY WIDTH (the reason it
	// exists), so the result must stay within the cell budget, not the rune/byte count.
	cjk := oneLine("你好世界你好", 4)
	if width(cjk) > 4 || !strings.HasSuffix(cjk, "…") {
		t.Errorf("CJK truncation = %q, want display width<=4 ending in …", cjk)
	}
	if got := oneLine("anything", 0); got != "" {
		t.Errorf("max<=0 should yield empty, got %q", got)
	}
}
