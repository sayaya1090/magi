package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/session"
)

func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// Global shortcuts: ctrl+l clears the transcript, ctrl+t toggles reasoning
// visibility, and ctrl+c requests shutdown.
func TestHandleKeyGlobalShortcuts(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.blocks = append(m.blocks, block{kind: blockInfo, text: "x"})
	m.liveText = "partial"

	if _, h := m.handleKey(ctrlKey('l')); !h {
		t.Fatal("ctrl+l should be handled")
	}
	if len(m.blocks) != 0 || m.liveText != "" {
		t.Errorf("ctrl+l should clear blocks/liveText, got %d blocks liveText=%q", len(m.blocks), m.liveText)
	}

	before := m.showThink
	if _, h := m.handleKey(ctrlKey('t')); !h {
		t.Fatal("ctrl+t should be handled")
	}
	if m.showThink == before {
		t.Error("ctrl+t should toggle showThink")
	}

	cmd, h := m.handleKey(ctrlKey('c'))
	if !h || cmd == nil || !m.quitting {
		t.Errorf("ctrl+c should quit; handled=%v cmd!=nil=%v quitting=%v", h, cmd != nil, m.quitting)
	}
}

// The resume picker captures navigation: up/down move the selection (clamped at
// both ends) and esc dismisses it. All keys are swallowed while it is open.
func TestHandleKeyResumePickerNav(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.resuming = true
	m.resumeList = []session.SessionMeta{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	m.resumeSel = 0

	down := tea.KeyPressMsg{Code: tea.KeyDown}
	up := tea.KeyPressMsg{Code: tea.KeyUp}

	m.handleKey(down)
	m.handleKey(down)
	if m.resumeSel != 2 {
		t.Errorf("two downs should land on row 2, got %d", m.resumeSel)
	}
	m.handleKey(down) // clamped at the last row
	if m.resumeSel != 2 {
		t.Errorf("down past the end should clamp at 2, got %d", m.resumeSel)
	}
	m.handleKey(up)
	m.handleKey(up)
	m.handleKey(up) // clamped at the top
	if m.resumeSel != 0 {
		t.Errorf("up past the top should clamp at 0, got %d", m.resumeSel)
	}

	// A random key is swallowed (handled) without leaving the picker.
	if _, h := m.handleKey(tea.KeyPressMsg{Code: 'x', Text: "x"}); !h || !m.resuming {
		t.Errorf("picker should swallow other keys; handled=%v resuming=%v", h, m.resuming)
	}

	// esc dismisses the picker.
	if _, h := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape}); !h || m.resuming {
		t.Errorf("esc should close the picker; handled=%v resuming=%v", h, m.resuming)
	}
}
