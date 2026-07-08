package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// pasteRE matches a collapsed-paste placeholder: [#12 pasted 34 lines].
var pasteRE = regexp.MustCompile(`\[#(\d+) pasted \d+ lines?\]`)

// pasteThreshold: pastes longer than this (or multi-line) are collapsed into a
// placeholder; shorter single-line pastes are inserted inline.
const pasteThreshold = 200

// handlePaste inserts pasted content into the input, collapsing large/multiline
// pastes into a [#N pasted L lines] placeholder while keeping
// the full text for expansion on send.
func (m *Model) handlePaste(content string) {
	// Normalize every newline flavor to "\n". Terminals deliver line breaks
	// inside a bracketed paste as CR (0x0D, the Enter byte), not LF, so handling
	// only "\r\n" left CR-separated pastes counted as one line and rendered with
	// raw CRs that overwrite the row on redraw (plexus#11).
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Count(content, "\n") + 1
	if lines <= 1 && len(content) <= pasteThreshold {
		m.ta.InsertString(content) // small inline paste
		return
	}
	if m.pastes == nil {
		m.pastes = map[int]string{}
	}
	m.pasteSeq++
	id := m.pasteSeq
	m.pastes[id] = content
	m.ta.InsertString(fmt.Sprintf("[#%d pasted %d lines]", id, lines))
}

// expandPastes replaces placeholders with their full stored content, so the
// agent receives the real pasted text even though the input shows a placeholder.
func (m *Model) expandPastes(text string) string {
	if len(m.pastes) == 0 {
		return text
	}
	return pasteRE.ReplaceAllStringFunc(text, func(match string) string {
		sub := pasteRE.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		id, _ := strconv.Atoi(sub[1])
		if content, ok := m.pastes[id]; ok {
			return content
		}
		return match
	})
}
