package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/change"
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
	blockShell          // a user-run `!`-prefixed shell command + its output
)

// block is one rendered unit in the transcript.
type block struct {
	kind blockKind
	// reqID is the origin MessageID of the user request a block belongs to (set on a
	// blockUser once its prompt.submitted event arrives). It doubles as the key for
	// pairing an inline answer with its question (moveUserBlockBefore) and for showing the
	// in-flight spinner on the request currently being processed. Empty on resume-rebuilt
	// blocks, which fall back to text matching.
	reqID string
	text  string // markdown (assistant/user) or pre-rendered content
	name  string // tool name (toolCall)
	args  string // tool args (toolCall)
	ok    bool   // tool success (toolResult, or a toolCall's attached result)
	// A tool result is folded into its toolCall block so the call renders as one
	// line whose leading glyph flips ⚙ → ✓/✗ on completion.
	done     bool   // the toolCall's result has arrived
	result   string // the toolCall's result summary text
	expanded bool   // a reasoning block individually expanded by a click
	// queued marks a blockUser that was submitted mid-turn (a steer) and is still waiting to be
	// handled: its bar renders as a distinct "queued" glyph rather than ▌ or the in-flight
	// spinner. Cleared when the message is answered inline (moveUserBlockBefore), resurfaces as
	// its own turn (moveUserBlockToEnd), or the turn finishes and the queue drains.
	queued bool
	// councilVerdicts carries a round's member votes for a blockCouncilVerdict block:
	// they render compact on ONE line, and a click opens the full-screen detail for
	// the member under the cursor. evidence is the pre-formatted "what the members saw
	// this round" (task/plan/report/diff), shown alongside the vote in the detail view.
	councilVerdicts []event.CouncilVerdictData
	evidence        string
}

// councilVerdictLabel maps a member's raw decision to UI wording by phase. The
// termination gate's "continue" is really a rejection (the result can't end the
// turn); a plan audit's "continue" is a revise request. (done/abstain unchanged.)
// councilVerdictLabel maps a vote to an icon + word. In the plan-audit phase a continue is
// tiered by severity so the result shows whether it forced a re-plan or was just advice:
// critical → revise (blocking), warn/absent → advise, info → note. Termination is done/reject.
func councilVerdictLabel(phase, decision, severity string) (icon, word string) {
	switch decision {
	case "done":
		if phase == "plan" {
			return "✓", "approve"
		}
		return "✓", "done"
	case "continue":
		if phase == "plan" {
			switch severity {
			case "", "warn": // absent severity normalizes to warn (non-blocking)
				return "✎", "advise"
			case "info":
				return "·", "note"
			default: // critical (or an unrecognized token → fail safe to blocking)
				return "↻", "revise"
			}
		}
		return "✗", "reject"
	case "abstain":
		return "∅", "abstain"
	}
	return "·", decision
}

// councilVerdictStyle gives a verdict its signal color, matching councilVerdictLabel's
// severity tiers: approve/done → green; (plan) advise → amber, note → muted, revise →
// red (blocking); (termination) reject → red; abstain/other → muted. Under NO_COLOR the
// word still carries the meaning.
func councilVerdictStyle(phase, decision, severity string) lipgloss.Style {
	switch decision {
	case "done":
		return styleToolOK
	case "continue":
		if phase == "plan" {
			switch severity {
			case "", "warn":
				return styleWarn // advise — amber
			case "info":
				return styleToolResult // note — muted
			default:
				return styleToolErr // revise (critical) — red, blocking
			}
		}
		return styleToolErr // reject — red
	case "abstain":
		return styleToolResult
	}
	return styleToolResult
}

// councilRowSep separates members in the one-line verdict row; its width must match
// what councilMemberPlain assumes when openCouncilDetailAt hit-tests a click column.
const councilRowSep = "   "

// councilMemberPlain is the visible (unstyled) text of one member's compact verdict —
// the same glyphs renderBlock styles — so a click column maps to the right member.
func councilMemberPlain(v event.CouncilVerdictData) string {
	icon, word := councilVerdictLabel(v.Phase, v.Decision, v.Severity)
	s := "● " + v.Member
	if v.Lens != "" {
		s += "  [" + v.Lens + "]"
	}
	s += "  " + icon + " " + word
	if v.Confidence > 0 {
		s += fmt.Sprintf(" · %.0f%%", v.Confidence*100)
	}
	return s
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
		// The in-flight request bubble shows an animated spinner, so it must NOT be served
		// from the static cache — render it fresh this frame (cheap: no glamour). The cache
		// keeps its ▌ version, which becomes correct again the moment the turn finishes.
		if m.running && m.turnReqID != "" && m.blocks[i].kind == blockUser && m.blocks[i].reqID == m.turnReqID {
			r = m.renderBlock(m.blocks[i])
		}
		b.WriteString(r)
		nl += strings.Count(r, "\n")
		b.WriteString("\n")
		nl++
	}
	m.liveThinkStart = -1
	if m.running && strings.TrimSpace(m.liveThink) != "" && strings.TrimSpace(m.liveText) == "" {
		b.WriteString("\n")
		nl++
		m.liveThinkStart = nl // the streaming thinking block is the last thing rendered; click it to fold
		if m.showThink {
			// Expanded: carry the same fold chip as a collapsed thought so "click to collapse"
			// is discoverable, not a hidden ctrl+t-only affordance.
			b.WriteString(label(styleBar, "thinking") + " " + styleFoldChip.Render(" click to collapse ") + "\n" + indent(m.wrapThink(strings.TrimRight(m.liveThink, "\n"))))
		} else {
			b.WriteString(indent(styleThink.Render("✻ thinking… ") + styleFoldChip.Render(" click / ctrl+t ")))
		}
		b.WriteString("\n")
	}
	if m.running && strings.TrimSpace(m.liveProgress) != "" {
		// A long-running tool's live status (wait_for polling). One ephemeral line by
		// the spinner so the wait is visible; cleared the moment the tool's result lands.
		b.WriteString("\n")
		b.WriteString(indent(styleThink.Render("⏳ " + m.liveProgress)))
		b.WriteString("\n")
		nl += 2
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

// userLabel is the display name for the user's transcript blocks: the name a plugin
// injected via magi.set_user_label, or "you" when none was set.
func (m *Model) userLabel() string {
	if m.userLbl != "" {
		return m.userLbl
	}
	return "you"
}

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
		// Wrap to the transcript width (like tool results and thinking) so a long
		// prompt soft-wraps instead of overflowing off-screen and being clipped.
		// Width is bodyWidth-2 to leave room for the 2-col indent().
		body := lipgloss.NewStyle().Width(m.bodyWidth() - 2).Render(strings.TrimRight(blk.text, "\n"))
		// The bar reflects the request's state: a spinner while its turn is being processed
		// (reverts to ▌ on finish; transcript() renders this block uncached each frame so it
		// animates), a distinct queued glyph while it waits mid-turn to be picked up, or ▌ at
		// rest. queuedGlyph is a single cell (like ▌) so the 2-col bar column stays aligned —
		// wide emoji would break width accounting.
		who := m.userLabel()
		lbl := label(styleUserLabel, who)
		switch {
		case m.running && blk.reqID != "" && blk.reqID == m.turnReqID:
			// spinner.Dot frames already carry a trailing space ("⣾ ", 2 cells) —
			// adding another made this state's bar column 3 cells while the ▌/·
			// states are 2, shifting the label right by one; on a wide (CJK) user
			// label the misaligned first paint can land mid-glyph.
			lbl = m.sp.View() + styleUserLabel.Render(who)
		case blk.queued:
			lbl = styleQueuedBar.Render(queuedGlyph+" ") + styleUserLabel.Render(who)
		}
		return lbl + copyChip() + "\n" + indent(body)
	case blockAssistant:
		return label(asstStyle, asstName) + copyChip() + "\n" + m.markdown(blk.text)
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
		// For an edit/write, show the actual change as a colorized diff beneath the
		// line (unless the call failed) — far clearer than a flattened arg preview.
		diff := ""
		if blk.ok || !blk.done {
			diff = change.EditDiff(blk.name, blk.args)
		}
		// Args preview: when the diff is shown the old/new/content is in it, so keep
		// only the path on the head line; otherwise the full compact preview.
		if diff != "" {
			if p := argPath(blk.args); p != "" {
				head += "  " + styleToolArgs.Render(p)
			}
		} else if a := compactArgs(blk.args); a != "" {
			head += "  " + styleToolArgs.Render(a)
		}
		if blk.done {
			if s := summarizeResult(blk.result); s != "" {
				head += styleToolResult.Render("  ⟶ " + s)
			}
		}
		if diff != "" {
			return indent(head) + "\n" + indent(m.renderCodeDiff(diff, rawPath(blk.args), m.bodyWidth()-2, diffBaseLine(blk)))
		}
		// Other tools (e.g. bash) show their output as a folded body beneath the line.
		if body := m.renderToolBody(blk); body != "" {
			return indent(head) + "\n" + indent(body)
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
		return indent(styleToolResult.Width(m.bodyWidth() - 2).Render(strings.TrimRight(blk.text, "\n")))
	case blockCouncilVerdict:
		// A round's members on ONE line, each in 기승전결 order: WHO (member) → LENS →
		// VERDICT → CONFIDENCE. Rationale/feedback stay hidden — clicking a member opens
		// its full detail (column hit-test in openCouncilDetailAt). Words kept for mono.
		if len(blk.councilVerdicts) == 0 {
			return indent(styleToolResult.Render(strings.TrimRight(blk.text, "\n")))
		}
		segs := make([]string, len(blk.councilVerdicts))
		for i, v := range blk.councilVerdicts {
			hue := m.councilColor(v.Member)
			icon, word := councilVerdictLabel(v.Phase, v.Decision, v.Severity)
			// Each member is CLICKABLE (column hit-test → detail modal), so it carries the
			// same low-emphasis container fill as the fold chip to read as tappable. The fill
			// must NOT change width — openCouncilDetailAt hit-tests against councilMemberPlain's
			// rune width — so we paint a background (no padding) and fold the interior spaces
			// into the painted runs, keeping the character layout identical to the plain form.
			paint := func(st lipgloss.Style, s string) string { return st.Background(colPrimCont).Render(s) }
			seg := paint(lipgloss.NewStyle().Foreground(hue), "● ") +
				paint(lipgloss.NewStyle().Foreground(hue).Bold(true), v.Member)
			if v.Lens != "" {
				seg += paint(styleToolResult, "  ["+v.Lens+"]")
			}
			seg += paint(councilVerdictStyle(v.Phase, v.Decision, v.Severity), "  "+icon+" "+word)
			if v.Confidence > 0 {
				seg += paint(styleToolResult, fmt.Sprintf(" · %.0f%%", v.Confidence*100))
			}
			segs[i] = seg
		}
		row := indent(strings.Join(segs, councilRowSep))
		// Below the one-line row, summarize WHY each member that voted to CONTINUE
		// (reject/revise) did so — feedback, else rationale — the reason that was
		// otherwise only visible by clicking the member. done/abstain add no line, so
		// the row stays compact when the council agrees (or merely abstains).
		var reasons []string
		for _, v := range blk.councilVerdicts {
			if v.Decision != "continue" {
				continue
			}
			reason := strings.TrimSpace(v.Feedback)
			if reason == "" {
				reason = strings.TrimSpace(v.Rationale)
			}
			if reason == "" {
				continue
			}
			hue := m.councilColor(v.Member)
			prefix := "  → " + v.Member + ": "
			pw := lipgloss.Width(prefix) // display width (CJK/wide member names count as 2)
			pad := strings.Repeat(" ", pw)
			// Wrap the reason to the transcript width with a hanging indent aligned under
			// the text (continuation lines line up past "→ Member: "), instead of cutting
			// it off with an ellipsis.
			wrapped := wrapLines(oneLine(reason, 100000), max(20, m.bodyWidth()-2-pw))
			label := lipgloss.NewStyle().Foreground(hue).Render(prefix)
			for k, ln := range wrapped {
				if k == 0 {
					reasons = append(reasons, indent(label+styleToolResult.Render(ln)))
				} else {
					reasons = append(reasons, indent(pad+styleToolResult.Render(ln)))
				}
			}
		}
		if len(reasons) > 0 {
			return row + "\n" + strings.Join(reasons, "\n")
		}
		return row
	case blockDiff:
		return label(styleAsstLabel, "diff") + "\n" + indent(colorizeDiff(blk.text))
	case blockReasoning:
		txt := strings.TrimRight(blk.text, "\n")
		if !m.showThink && !blk.expanded {
			// Collapsed by default: a dim one-liner preview. Click it to expand just
			// this one, or ctrl+t to expand all. Size the preview to the transcript
			// width so it never overflows a narrow (panel-shrunk) transcript.
			prev := clampInt(m.transcriptWidth()-34, 16, 64)
			return indent(styleThink.Render("✻ thought · "+oneLine(txt, prev)+" ") + styleFoldChip.Render(" click / ctrl+t "))
		}
		return label(styleBar, "thinking") + "\n" + indent(m.wrapThink(txt))
	case blockImage:
		return indent(blk.text) // pre-rendered half-block pixels
	case blockShell:
		// A `!`-run command: an accent "$ cmd" header, the combined output as a dim
		// clipped body, and a trailing "exit N" (red when non-zero). blk.args holds the
		// command, blk.text the output, blk.result the "exit N" label, blk.ok exit==0.
		head := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render("$ ") +
			lipgloss.NewStyle().Foreground(colAccent).Render(oneLine(blk.args, 200))
		w := m.bodyWidth() - 2
		exitStyle := styleToolResult
		if !blk.ok {
			exitStyle = styleError
		}
		body := indent(head)
		if out := strings.TrimRight(blk.text, "\n"); strings.TrimSpace(out) != "" {
			var lines []string
			for _, ln := range strings.Split(out, "\n") {
				lines = append(lines, styleToolResult.Render(clipLine(ln, w))) // clipLine strips control seqs
			}
			body += "\n" + indent(strings.Join(lines, "\n"))
		}
		body += "\n" + indent(exitStyle.Render(blk.result))
		return body
	}
	return ""
}

// stripControl removes terminal control sequences from untrusted content (file
// contents, command output) before it is rendered into the transcript, so an
// embedded escape can't move the cursor, clear the screen, or spoof the window
// title (audit finding N10). It drops ANSI/OSC escapes via ansi.Strip, then any
// remaining C0/C1 control bytes except tab (clipLine expands tabs to spaces).
func stripControl(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
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

// copyChip is the per-block copy button appended to a user/assistant label line:
// a low-emphasis fold-style chip holding ⧉ (U+29C9, East-Asian-Neutral — renders one
// cell everywhere, unlike the ambiguous-width clipboard glyphs). Clicking it copies
// the block's SOURCE text (raw markdown, not the styled render); the hit-test in
// copyBlockAt mirrors this exact geometry: label width + one space + a 3-cell chip.
func copyChip() string {
	return " " + styleFoldChip.Render("⧉")
}

// queuedGlyph marks a mid-turn queued user bubble's bar. It must be a single terminal cell
// (like ▌ and the braille spinner) so the 2-col bar column stays aligned — wide/emoji glyphs
// mis-measure and shift the layout. · (middle dot) reads as a quiet "waiting" marker.
const queuedGlyph = "·"

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

// bodyWidth is the width actually available to transcript CONTENT lines. With
// the drawn scrollbar retired (scroll position lives in the header chip) it
// equals transcriptWidth; it stays a named seam so block renderers keep one
// authoritative content width should a gutter ever return.
func (m *Model) bodyWidth() int { return m.transcriptWidth() }

// wrapThink word-wraps "thinking" text to the transcript CONTENT width minus
// the 2-col indent applied by indent(). It must match the markdown body's wrap
// width (buildGlam), or the padded think lines run wider than the content area.
func (m *Model) wrapThink(s string) string {
	return styleThink.Width(m.bodyWidth() - 2).Render(s)
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

// argPath returns the "path=…" preview for a tool call, or "" if it has none.
func argPath(args string) string {
	if p := rawPath(args); p != "" {
		return "path=" + oneLine(p, 80)
	}
	return ""
}

// diffBaseLine is the 1-based file line the diff's first line maps to, for the
// new-side gutter: 1 for a write (whole file), the edit tool's reported " @N" start
// line for an edit, or 0 (no gutter) when the position is unknown.
func diffBaseLine(blk block) int {
	switch blk.name {
	case "write":
		return 1
	case "edit":
		return parseTrailingAt(blk.result)
	}
	return 0
}

// parseTrailingAt reads a trailing " @<digits>" marker (the edit tool's start line),
// returning the number or 0 if absent.
func parseTrailingAt(s string) int {
	s = strings.TrimRight(s, " \n")
	j := len(s)
	for j > 0 && s[j-1] >= '0' && s[j-1] <= '9' {
		j--
	}
	if j == len(s) || j < 2 || s[j-2:j] != " @" {
		return 0
	}
	n := 0
	for _, r := range s[j:] {
		n = n*10 + int(r-'0')
	}
	return n
}

// rawPath returns a tool call's raw "path" arg (for language detection), or "".
func rawPath(args string) string {
	var a struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(args), &a) == nil {
		return a.Path
	}
	return ""
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
	// Newlines first (stripControl drops \n outright, which would fuse adjacent
	// words), then strip terminal control sequences so untrusted preview/header
	// content — model thoughts, tool args/results, subagent snippets — can't move
	// the cursor, clear the screen, or spoof the title (audit finding N10). The
	// transcript BODY path is already guarded by clipLine; oneLine is the matching
	// choke point for the one-line preview/header paths that skip clipLine.
	s = strings.ReplaceAll(s, "\n", " ")
	s = stripControl(s)
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 {
		return ""
	}
	// Truncate by DISPLAY WIDTH (handles wide CJK/emoji and ANSI), keeping the result
	// — ellipsis included — within max cells, so callers' width budgets aren't overrun
	// (an overrun wrapped panel rows and broke click hit-testing).
	return ansi.Truncate(s, max, "…")
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

// moveUserBlockToEnd relocates the matching blockUser to the end of the slice, so a
// re-surfacing queued interjection's query renders just above its incoming answer (Q&A
// pairing). It matches by reqID first (the resurfaced prompt's ResurfacedFrom == the
// original block's reqID), falling back to text for resume-rebuilt blocks that carry no
// reqID. If nothing matches it appends a fresh user block as a safe fallback.
func moveUserBlockToEnd(blocks []block, reqID, text string) []block {
	if i := findUserBlock(blocks, reqID, text); i >= 0 {
		b := blocks[i]
		b.queued = false // it now runs as its own turn (shows the spinner), no longer waiting
		blocks = append(blocks[:i], blocks[i+1:]...)
		return append(blocks, b)
	}
	return append(blocks, block{kind: blockUser, reqID: reqID, text: strings.TrimSpace(text)})
}

// moveUserBlockBefore relocates the matching blockUser to just before position idx (the
// index of the inline answer that replies to it), so an interjection answered inline —
// which never resurfaces as its own turn — still reads as a [question → answer] pair
// pulled out of the main-task flow, question on top. A no-op if no block matches or the
// block is already immediately before idx. reqID-only (inline answers always have one).
func moveUserBlockBefore(blocks []block, reqID string, idx int) ([]block, bool) {
	if reqID == "" || idx <= 0 || idx > len(blocks) {
		return blocks, false
	}
	i := -1
	for j := idx - 1; j >= 0; j-- {
		if blocks[j].kind == blockUser && blocks[j].reqID == reqID {
			i = j
			break
		}
	}
	if i < 0 || i == idx-1 {
		return blocks, false // no match, or already adjacent above the answer
	}
	b := blocks[i]
	b.queued = false // answered inline — it's handled, no longer waiting in the queue
	blocks = append(blocks[:i], blocks[i+1:]...)
	// Removing i (which is < idx) shifts the answer down by one, so the slot just before
	// the answer is now idx-1.
	out := make([]block, 0, len(blocks)+1)
	out = append(out, blocks[:idx-1]...)
	out = append(out, b)
	out = append(out, blocks[idx-1:]...)
	return out, true
}

// stampUserReqID records reqID on the OLDEST reqID-less blockUser that matches text,
// binding a locally-added request bubble to its origin MessageID once the prompt.submitted
// event arrives. prompt.submitted events arrive in submit order, so the first-arriving one
// must claim the oldest unstamped bubble — a newest-first scan would swap the reqIDs of two
// pending same-text bubbles. Returns whether a block was stamped.
func stampUserReqID(blocks []block, text, reqID string) bool {
	text = strings.TrimSpace(text)
	for i := range blocks {
		if blocks[i].kind == blockUser && blocks[i].reqID == "" && strings.TrimSpace(blocks[i].text) == text {
			blocks[i].reqID = reqID
			return true
		}
	}
	return false
}

// findUserBlock returns the index of the matching blockUser (reqID first, then text), or -1.
func findUserBlock(blocks []block, reqID, text string) int {
	if reqID != "" {
		for i := len(blocks) - 1; i >= 0; i-- {
			if blocks[i].kind == blockUser && blocks[i].reqID == reqID {
				return i
			}
		}
	}
	text = strings.TrimSpace(text)
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].kind == blockUser && strings.TrimSpace(blocks[i].text) == text {
			return i
		}
	}
	return -1
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
