package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// maxToolBodyLines is how many lines of a tool's output are shown collapsed; the
// rest fold behind a "+N more" footer that a click (or ctrl+t) expands.
const maxToolBodyLines = 16

// renderToolBody returns the multi-line body shown under a tool-call line (e.g. a
// bash command's output), folded to maxToolBodyLines unless the block is expanded.
// Returns "" for tools with no body worth showing beyond the one-line summary.
func (m *Model) renderToolBody(blk block) string {
	lines := toolBodyLines(blk)
	if len(lines) == 0 {
		return ""
	}
	return foldLines(lines, blk.expanded, m.transcriptWidth()-2, styleToolResult)
}

// toolBodyLines extracts the raw (unstyled) body lines for a tool result, or nil if
// the tool has no expandable body. New body-rendering tools are added here.
func toolBodyLines(blk block) []string {
	if !blk.done {
		return nil
	}
	switch blk.name {
	case "bash":
		return bashOutputLines(blk.result)
	}
	return nil
}

// toolBodyOverflows reports whether a tool block has more body than fits collapsed —
// i.e. clicking it would meaningfully expand/collapse it.
func toolBodyOverflows(blk block) bool {
	return len(toolBodyLines(blk)) > maxToolBodyLines
}

// bashOutputLines strips the bash tool's leading "exit N" status line (already shown
// as the head summary) and returns the command output as lines, or nil if empty.
func bashOutputLines(result string) []string {
	out := result
	if strings.HasPrefix(out, "exit ") {
		if nl := strings.IndexByte(out, '\n'); nl >= 0 {
			out = out[nl+1:]
		} else {
			out = ""
		}
	}
	out = strings.TrimRight(out, "\n")
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// foldLines renders body lines with style, each clipped to width, capped to
// maxToolBodyLines unless expanded; when capped it appends a dim "+N more" footer.
func foldLines(lines []string, expanded bool, width int, style lipgloss.Style) string {
	shown, hidden := lines, 0
	if !expanded && len(lines) > maxToolBodyLines {
		shown = lines[:maxToolBodyLines]
		hidden = len(lines) - maxToolBodyLines
	}
	var b strings.Builder
	for i, ln := range shown {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(style.Render(clipLine(ln, width)))
	}
	switch {
	case hidden > 0:
		b.WriteString("\n" + styleThink.Render(fmt.Sprintf("… +%d more lines · click to expand", hidden)))
	case expanded && len(lines) > maxToolBodyLines:
		b.WriteString("\n" + styleThink.Render("· click to collapse"))
	}
	return b.String()
}

// clipLine expands tabs and truncates a single line to width (rune-aware) so long
// output can't break the transcript layout.
func clipLine(s string, width int) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	if width <= 1 {
		return s
	}
	if r := []rune(s); len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}
