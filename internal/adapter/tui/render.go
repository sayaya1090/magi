package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// blockKind classifies a transcript block for rendering.
type blockKind int

const (
	blockUser blockKind = iota
	blockAssistant
	blockToolCall
	blockToolResult
	blockError
	blockInfo           // slash-command output / system notices
	blockDiff           // git diff output (colorized)
	blockReasoning      // model "thinking" output (de-emphasized)
	blockImage          // pre-rendered image (half-block)
	blockCouncilVerdict // one council member's vote (compact; click → detail modal)
)

// block is one rendered unit in the transcript.
type block struct {
	kind blockKind
	text string // markdown (assistant/user) or pre-rendered content
	name string // tool name (toolCall)
	args string // tool args (toolCall)
	ok   bool   // tool success (toolResult, or a toolCall's attached result)
	// A tool result is folded into its toolCall block so the call renders as one
	// line whose leading glyph flips ⚙ → ✓/✗ on completion.
	done     bool   // the toolCall's result has arrived
	result   string // the toolCall's result summary text
	expanded bool   // a reasoning block individually expanded by a click
	// councilVerdict carries one member's full vote for a blockCouncilVerdict block:
	// the line renders compact, and a click opens the full-screen detail from this
	// data. evidence is the pre-formatted "what the members saw this round"
	// (task/plan/report/diff), shown alongside the vote in the detail view.
	councilVerdict *event.CouncilVerdictData
	evidence       string
}

// councilVerdictLabel maps a member's raw decision to UI wording by phase. The
// termination gate's "continue" is really a rejection (the result can't end the
// turn); a plan audit's "continue" is a revise request. (done/abstain unchanged.)
func councilVerdictLabel(phase, decision string) (icon, word string) {
	switch decision {
	case "done":
		if phase == "plan" {
			return "✓", "approve"
		}
		return "✓", "done"
	case "continue":
		if phase == "plan" {
			return "↻", "revise"
		}
		return "✗", "reject"
	case "abstain":
		return "∅", "abstain"
	}
	return "·", decision
}

// councilVerdictStyle gives a verdict its signal color: approve/done → success
// (green), revise → caution (amber), reject → error (red), abstain/other → muted.
// Phase distinguishes a plan "revise" (amber) from a termination "reject" (red),
// matching councilVerdictLabel. Under NO_COLOR the word still carries the meaning.
func councilVerdictStyle(phase, decision string) lipgloss.Style {
	switch decision {
	case "done":
		return styleToolOK
	case "continue":
		if phase == "plan" {
			return styleWarn
		}
		return styleToolErr
	case "abstain":
		return styleToolResult
	}
	return styleToolResult
}

// transcript renders the full transcript plus any in-progress streaming text.
// Finalized blocks are rendered once and cached (keyed by width), so streaming a
// token does NOT re-run markdown rendering over the whole history — that
// re-layout per token is what causes the "rippling" flicker. The in-progress
// block is rendered cheaply (no markdown) while streaming.
func (m *Model) transcript() string {
	// Key the cache by the TRANSCRIPT width (terminal minus the side panel), not
	// the raw terminal width — otherwise dragging the panel splitter (which keeps
	// m.width constant) leaves blocks wrapped to the stale width.
	if tw := m.transcriptWidth(); m.cacheW != tw {
		m.cache = m.cache[:0]
		m.cacheW = tw
	}
	for i := len(m.cache); i < len(m.blocks); i++ {
		m.cache = append(m.cache, m.renderBlock(m.blocks[i]))
	}

	var b strings.Builder
	m.blockLineStart = m.blockLineStart[:0]
	nl := 0 // newlines written so far == content line index of the next char
	for i, r := range m.cache {
		if i > 0 {
			b.WriteString("\n")
			nl++
		}
		m.blockLineStart = append(m.blockLineStart, nl) // line where block i starts
		b.WriteString(r)
		nl += strings.Count(r, "\n")
		b.WriteString("\n")
		nl++
	}
	if m.running && strings.TrimSpace(m.liveThink) != "" && strings.TrimSpace(m.liveText) == "" {
		b.WriteString("\n")
		if m.showThink {
			b.WriteString(label(styleBar, "thinking") + "\n" + indent(m.wrapThink(strings.TrimRight(m.liveThink, "\n"))))
		} else {
			b.WriteString(indent(styleThink.Render("✻ thinking… · ctrl+t to expand")))
		}
		b.WriteString("\n")
	}
	if m.running && strings.TrimSpace(m.liveText) != "" {
		b.WriteString("\n")
		b.WriteString(m.renderLive(m.liveText))
		b.WriteString("\n")
	}
	return b.String()
}

// renderLive renders the streaming assistant text WITH markdown styling so the
// style applies live, not only once the turn finishes. Only this bottom block
// re-renders per frame (history blocks are cached), and repaints are throttled to
// one per render tick — so styling no longer reflows the whole transcript. The
// finalized block renders identically, so there's no style "snap" at the end.
func (m *Model) renderLive(s string) string {
	return label(styleAsstLabel, "magi") + "\n" + m.markdown(balanceFences(s))
}

// balanceFences closes a dangling ``` code fence so glamour can syntax-highlight
// (color) the code block WHILE it streams, instead of only once the closing
// fence arrives. Applied to live text only.
func balanceFences(s string) string {
	if strings.Count(s, "```")%2 == 1 {
		return s + "\n```"
	}
	return s
}

// foldToolResult attaches a tool result to the most recent toolCall block that is
// still pending, so the call renders as a single line with a flipped glyph. If no
// such call exists (e.g. a result without a recorded call), it falls back to a
// standalone result block. It invalidates the affected cache entries.
func (m *Model) foldToolResult(text string, ok bool) {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := &m.blocks[i]
		if b.kind == blockToolCall && !b.done {
			b.done = true
			b.ok = ok
			b.result = text
			if len(m.cache) > i {
				m.cache = m.cache[:i] // re-render this (now-completed) call line
			}
			return
		}
		// Stop scanning past a non-tool boundary (assistant text) to avoid folding
		// into a call from an earlier message.
		if b.kind == blockAssistant || b.kind == blockUser {
			break
		}
	}
	m.blocks = append(m.blocks, block{kind: blockToolResult, text: text, ok: ok})
}

func (m *Model) renderBlock(blk block) string { return m.renderBlockAs(blk, "magi", nil) }

// renderBlockAs renders a block, labelling assistant output with asstName (and
// asstColor if set) — used so a subagent's detail view attributes lines to that
// agent instead of "magi".
func (m *Model) renderBlockAs(blk block, asstName string, asstColor color.Color) string {
	asstStyle := styleAsstLabel
	if asstColor != nil {
		asstStyle = lipgloss.NewStyle().Foreground(asstColor).Bold(true)
	}
	switch blk.kind {
	case blockUser:
		return label(styleUserLabel, "you") + "\n" + indent(strings.TrimRight(blk.text, "\n"))
	case blockAssistant:
		return label(asstStyle, asstName) + "\n" + m.markdown(blk.text)
	case blockToolCall:
		// Leading glyph reflects state: ⚙ while running, ✓/✗ once the result is in
		// (the result is folded onto this same line — no separate result line).
		glyph := styleToolName.Render("⚙")
		if blk.done {
			if blk.ok {
				glyph = styleToolOK.Render("✓")
			} else {
				glyph = styleToolErr.Render("✗")
			}
		}
		// The task tool delegates to subagents — surface the target agent name(s)
		// in each agent's assigned color so the transcript ties to the live panes.
		if blk.name == "task" {
			if agents := taskAgents(blk.args); len(agents) > 0 {
				colored := make([]string, len(agents))
				for i, a := range agents {
					colored[i] = lipgloss.NewStyle().Foreground(m.paneColor(a)).Bold(true).Render(a)
				}
				return indent(glyph + styleToolName.Render(" task → ") + strings.Join(colored, ", "))
			}
		}
		head := glyph + " " + styleToolName.Render(blk.name)
		if a := compactArgs(blk.args); a != "" {
			head += "  " + styleToolArgs.Render(a)
		}
		if blk.done {
			if s := summarizeResult(blk.result); s != "" {
				head += styleToolResult.Render("  ⟶ " + s)
			}
		}
		return indent(head)
	case blockToolResult:
		// Fallback: a result with no matching call (foldToolResult appends this).
		mark := styleToolOK.Render("✓")
		if !blk.ok {
			mark = styleToolErr.Render("✗")
		}
		return indent(mark + " " + styleToolResult.Render(summarizeResult(blk.text)))
	case blockError:
		return indent(styleError.Render("✗ " + blk.text))
	case blockInfo:
		// Wrap to the transcript width (minus the 2-col indent) so a long line
		// (e.g. the planner's reason) reflows instead of overflowing.
		return indent(styleToolResult.Width(m.transcriptWidth() - 2).Render(strings.TrimRight(blk.text, "\n")))
	case blockCouncilVerdict:
		// One-line summary in a 기승전결 order: WHO (member) → through which LENS →
		// the VERDICT → with what CONFIDENCE. The rationale/feedback stay hidden —
		// a click opens the full detail. (Decision word kept for NO_COLOR/mono.)
		v := blk.councilVerdict
		if v == nil {
			return indent(styleToolResult.Render(strings.TrimRight(blk.text, "\n")))
		}
		hue := m.councilColor(v.Member)
		dot := lipgloss.NewStyle().Foreground(hue).Render("●")
		name := lipgloss.NewStyle().Foreground(hue).Bold(true).Render(v.Member)
		icon, word := councilVerdictLabel(v.Phase, v.Decision)
		line := dot + " " + name
		if v.Lens != "" {
			line += "  " + styleToolResult.Render("["+v.Lens+"]")
		}
		line += "  " + councilVerdictStyle(v.Phase, v.Decision).Render(icon+" "+word)
		if v.Confidence > 0 {
			line += styleToolResult.Render(fmt.Sprintf(" · %.0f%%", v.Confidence*100))
		}
		return indent(line)
	case blockDiff:
		return label(styleAsstLabel, "diff") + "\n" + indent(colorizeDiff(blk.text))
	case blockReasoning:
		txt := strings.TrimRight(blk.text, "\n")
		if !m.showThink && !blk.expanded {
			// Collapsed by default: a dim one-liner preview. Click it to expand just
			// this one, or ctrl+t to expand all. Size the preview to the transcript
			// width so it never overflows a narrow (panel-shrunk) transcript.
			prev := clampInt(m.transcriptWidth()-34, 16, 64)
			return indent(styleThink.Render("✻ thought · " + oneLine(txt, prev) + " · click / ctrl+t"))
		}
		return label(styleBar, "thinking") + "\n" + indent(m.wrapThink(txt))
	case blockImage:
		return indent(blk.text) // pre-rendered half-block pixels
	}
	return ""
}

// markdown renders assistant text as wrapped, syntax-highlighted markdown,
// falling back to indented plain text if the renderer is unavailable.
func (m *Model) markdown(s string) string {
	s = strings.TrimRight(s, "\n")
	if m.glam == nil {
		return indent(s)
	}
	out, err := m.glam.Render(s)
	if err != nil {
		return indent(s)
	}
	return strings.TrimRight(out, "\n")
}

func label(style lipgloss.Style, name string) string {
	return styleBar.Render("▌ ") + style.Render(name)
}

// transcriptWidth is the column width available to transcript content — the
// terminal width minus the right side panel (and its gap). Floored so callers
// can subtract a small indent without going negative.
func (m *Model) transcriptWidth() int {
	w := m.width - m.panelCols()
	if w < 24 {
		w = 24
	}
	return w
}

// wrapThink word-wraps "thinking" text to the transcript width (accounting for
// the 2-col indent applied by indent()) so long reasoning lines reflow instead
// of overflowing the panel/viewport. styleThink is applied per wrapped line.
func (m *Model) wrapThink(s string) string {
	return styleThink.Width(m.transcriptWidth() - 2).Render(s)
}

func indent(s string) string {
	var b strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  " + line)
	}
	return b.String()
}

// compactArgs renders tool args compactly (single line, key:value-ish).
func compactArgs(args string) string {
	args = strings.TrimSpace(args)
	if args == "" || args == "{}" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return oneLine(args, 80)
	}
	// Sort keys so the rendered order is stable — Go map iteration is randomized,
	// which otherwise reshuffles the args every frame and makes the line flicker.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		sv := oneLine(toStr(m[k]), 60)
		parts = append(parts, k+"="+sv)
	}
	return oneLine(strings.Join(parts, " "), 100)
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// userPrompts extracts user-authored prompt texts from a reconstructed
// transcript, for seeding input history (↑/↓ recall + tab completion) on resume.
// Injected subagent results (actor agent) are user-role too but are skipped by
// excluding the "[subagent " prefix.
func userPrompts(msgs []session.Message) []string {
	var out []string
	for _, msg := range msgs {
		if msg.Role != session.RoleUser {
			continue
		}
		t := strings.TrimSpace(joinTextParts(msg.Parts))
		if t == "" || strings.HasPrefix(t, "[subagent ") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// rebuildBlocks converts reconstructed messages into transcript blocks (used
// when resuming a session).
func rebuildBlocks(msgs []session.Message) []block {
	var out []block
	for _, msg := range msgs {
		switch msg.Role {
		case session.RoleUser:
			out = append(out, block{kind: blockUser, text: joinTextParts(msg.Parts)})
		case session.RoleSystem:
			if t := joinTextParts(msg.Parts); t != "" {
				out = append(out, block{kind: blockInfo, text: t})
			}
		case session.RoleAssistant:
			for _, p := range msg.Parts {
				switch p.Kind {
				case session.PartReasoning:
					out = append(out, block{kind: blockReasoning, text: p.Text})
				case session.PartText:
					if p.Text != "" {
						out = append(out, block{kind: blockAssistant, text: p.Text})
					}
				case session.PartToolCall:
					if p.ToolCall != nil {
						out = append(out, block{kind: blockToolCall, name: p.ToolCall.Name, args: string(p.ToolCall.Args)})
					}
				}
			}
		case session.RoleTool:
			for _, p := range msg.Parts {
				if p.Kind == session.PartToolResult && p.ToolResult != nil {
					out = foldToolResultInto(out, toolResultText(p.ToolResult), !p.ToolResult.IsError)
				}
			}
		}
	}
	return out
}

// foldToolResultInto attaches a tool result to the most recent pending toolCall
// block (mirrors Model.foldToolResult for the resume/rebuild path).
func foldToolResultInto(out []block, text string, ok bool) []block {
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].kind == blockToolCall && !out[i].done {
			out[i].done = true
			out[i].ok = ok
			out[i].result = text
			return out
		}
		if out[i].kind == blockAssistant || out[i].kind == blockUser {
			break
		}
	}
	return append(out, block{kind: blockToolResult, text: text, ok: ok})
}

func joinTextParts(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind == session.PartText {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// summarizeResult renders a tool result compactly for the human (the model
// still receives the full result). JSON arrays become "N items: a, b, …";
// objects/text are shown as a trimmed first line.
func summarizeResult(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "[") {
		var arr []json.RawMessage
		if json.Unmarshal([]byte(t), &arr) == nil {
			if len(arr) == 0 {
				return "(none)"
			}
			names := make([]string, 0, 5)
			for i, e := range arr {
				if i >= 5 {
					break
				}
				names = append(names, itemLabel(e))
			}
			more := ""
			if len(arr) > 5 {
				more = ", …"
			}
			return fmt.Sprintf("%d items: %s%s", len(arr), strings.Join(names, ", "), more)
		}
	}
	// First non-empty line, trimmed.
	for _, line := range strings.Split(t, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return oneLine(s, 120)
		}
	}
	return ""
}

// itemLabel extracts a short label from a JSON array element (name field, or raw).
func itemLabel(e json.RawMessage) string {
	var obj struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if json.Unmarshal(e, &obj) == nil {
		if obj.Name != "" {
			return obj.Name
		}
		if obj.Path != "" {
			return obj.Path
		}
	}
	var s string
	if json.Unmarshal(e, &s) == nil {
		return oneLine(s, 40)
	}
	return oneLine(string(e), 40)
}

// colorizeDiff applies green/red/cyan styling to unified-diff lines.
func colorizeDiff(s string) string {
	var b strings.Builder
	for i, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			b.WriteString(styleToolOK.Render(line))
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			b.WriteString(styleToolErr.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(styleToolName.Render(line))
		case strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			b.WriteString(styleAsstLabel.Render(line))
		default:
			b.WriteString(styleToolResult.Render(line))
		}
	}
	return b.String()
}

// toolResultText extracts a displayable string from a tool result payload.
func toolResultText(tr *session.ToolResult) string {
	if tr == nil {
		return ""
	}
	var s string
	if json.Unmarshal(tr.Content, &s) == nil {
		return s
	}
	return string(tr.Content)
}
