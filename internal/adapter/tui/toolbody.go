package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// maxToolBodyLines is how many lines of a tool's output are shown collapsed; the
// rest fold behind a "+N more" footer that a click expands.
const maxToolBodyLines = 16

// renderToolBody returns the multi-line body shown under a tool-call line (bash
// output, a read'd file, grep/glob/list results), folded to maxToolBodyLines unless
// the block is expanded. Returns "" for tools with no body worth showing.
func (m *Model) renderToolBody(blk block) string {
	lines := m.toolBody(blk)
	if len(lines) == 0 {
		return ""
	}
	return foldRendered(lines, blk.expanded)
}

// toolBody dispatches to the per-tool body renderer, returning fully styled lines
// (already clipped to width). New body-rendering tools are added here.
func (m *Model) toolBody(blk block) []string {
	if !blk.done {
		return nil
	}
	switch blk.name {
	case "bash":
		return m.bashBody(blk)
	case "read":
		return m.readBody(blk)
	case "grep":
		return m.grepBody(blk)
	case "glob":
		return m.globBody(blk)
	case "list":
		return m.listBody(blk)
	}
	return nil
}

// toolBodyOverflows reports whether a tool block has more body than fits collapsed —
// i.e. clicking it would meaningfully expand/collapse it.
func (m *Model) toolBodyOverflows(blk block) bool {
	return len(m.toolBody(blk)) > maxToolBodyLines
}

// foldRendered caps already-styled body lines to maxToolBodyLines unless expanded,
// appending a dim "+N more" (or collapse) footer.
func foldRendered(lines []string, expanded bool) string {
	shown, hidden := lines, 0
	if !expanded && len(lines) > maxToolBodyLines {
		shown = lines[:maxToolBodyLines]
		hidden = len(lines) - maxToolBodyLines
	}
	body := strings.Join(shown, "\n")
	switch {
	case hidden > 0:
		body += "\n" + styleThink.Render(fmt.Sprintf("… +%d more lines · click to expand", hidden))
	case expanded && len(lines) > maxToolBodyLines:
		body += "\n" + styleThink.Render("· click to collapse")
	}
	return body
}

// bashBody renders a bash command's output (minus the "exit N" status line, which is
// the head summary) as a dim block.
func (m *Model) bashBody(blk block) []string {
	raw := bashOutputLines(blk.result)
	w := m.transcriptWidth() - 2
	out := make([]string, len(raw))
	for i, ln := range raw {
		out[i] = styleToolResult.Render(clipLine(ln, w))
	}
	return out
}

// readBody renders a read'd file: a dim line-number gutter (kept from the tool's
// cat -n output) plus the code syntax-highlighted by chroma (language from the path).
func (m *Model) readBody(blk block) []string {
	lexer := lexerFor(rawPath(blk.args))
	st := m.codeStyle()
	w := m.transcriptWidth() - 2
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(blk.result, "\n"), "\n") {
		gutter, code, ok := splitNumberedLine(ln)
		if !ok {
			continue // the internal "(note: …)" prefix etc. — not a content line
		}
		codeW := w - len([]rune(gutter)) - 1
		seg := styleThink.Render(gutter+" ") + highlightTokens(clipLine(code, codeW), lexer, st, lipgloss.NewStyle())
		out = append(out, seg)
	}
	return out
}

// grepBody renders grep matches ("rel:line:text") as "file:line" in accent + the
// matched text.
func (m *Model) grepBody(blk block) []string {
	matches, ok := jsonStrings(blk.result)
	if !ok {
		return nil
	}
	loc := lipgloss.NewStyle().Foreground(colAccent)
	w := m.transcriptWidth() - 2
	out := make([]string, 0, len(matches))
	for _, mtch := range matches {
		parts := strings.SplitN(mtch, ":", 3)
		if len(parts) < 3 {
			out = append(out, styleToolResult.Render(clipLine(mtch, w)))
			continue
		}
		head := parts[0] + ":" + parts[1]
		text := strings.TrimSpace(parts[2])
		out = append(out, loc.Render(head)+"  "+styleToolResult.Render(clipLine(text, max(8, w-len(head)-2))))
	}
	return out
}

// globBody renders matched file paths as a dim, bulleted list.
func (m *Model) globBody(blk block) []string {
	paths, ok := jsonStrings(blk.result)
	if !ok {
		return nil
	}
	w := m.transcriptWidth() - 2
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = styleToolResult.Render("• " + clipLine(p, w-2))
	}
	return out
}

// listBody renders a directory listing: directories first (accent, trailing "/"),
// then files (dim).
func (m *Model) listBody(blk block) []string {
	var entries []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(blk.result)), &entries) != nil {
		return nil
	}
	dirStyle := lipgloss.NewStyle().Foreground(colAccent)
	w := m.transcriptWidth() - 2
	out := make([]string, len(entries))
	for i, e := range entries {
		if e.IsDir {
			out[i] = dirStyle.Render(clipLine(e.Name+"/", w))
		} else {
			out[i] = styleToolResult.Render(clipLine(e.Name, w))
		}
	}
	return out
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

// splitNumberedLine parses a "<spaces><digits>\t<code>" line (read's cat -n output)
// into its gutter (the spaces+digits, kept for alignment) and code, or ok=false.
func splitNumberedLine(s string) (gutter, code string, ok bool) {
	tab := strings.IndexByte(s, '\t')
	if tab <= 0 {
		return "", "", false
	}
	digits := strings.TrimSpace(s[:tab])
	if digits == "" {
		return "", "", false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", "", false
		}
	}
	return s[:tab], s[tab+1:], true
}

// jsonStrings decodes a JSON array of strings (grep/glob results), or ok=false.
func jsonStrings(result string) ([]string, bool) {
	var a []string
	if json.Unmarshal([]byte(strings.TrimSpace(result)), &a) == nil {
		return a, true
	}
	return nil, false
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
