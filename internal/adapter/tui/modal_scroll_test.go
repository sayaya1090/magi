package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// scrollTranscriptKey moves the viewport for scroll keys and reports non-scroll keys
// as unhandled, so modal handlers can page the transcript without swallowing input.
func TestScrollTranscriptKey(t *testing.T) {
	m := newTestModel(t)
	m.vp.SetWidth(40)
	m.vp.SetHeight(3)
	m.vp.SetContent(strings.Repeat("line\n", 50))
	m.vp.GotoBottom()

	before := m.vp.YOffset()
	if before == 0 {
		t.Fatalf("setup: viewport should start scrolled to the bottom")
	}
	if !m.scrollTranscriptKey("pgup") {
		t.Fatalf("pgup should be handled")
	}
	if m.vp.YOffset() >= before {
		t.Errorf("pgup did not scroll up: %d → %d", before, m.vp.YOffset())
	}
	if m.scrollTranscriptKey("j") {
		t.Errorf("a non-scroll key should report unhandled")
	}
}

// While a permission modal is open, a scroll key pages the transcript instead of being
// swallowed — and the modal stays open so the decision is still pending.
func TestPermissionModalStaysScrollable(t *testing.T) {
	m := newTestModel(t)
	m.vp.SetWidth(40)
	m.vp.SetHeight(3)
	m.vp.SetContent(strings.Repeat("line\n", 50))
	m.vp.GotoBottom()
	m.perm = &permReq{callID: "c1", name: "bash", args: "{}", reason: "test"}

	before := m.vp.YOffset()
	if _, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgUp}); !handled {
		t.Fatal("pgup should be handled while the modal is open")
	}
	if m.perm == nil {
		t.Fatal("a scroll key must not dismiss the permission modal")
	}
	if m.vp.YOffset() >= before {
		t.Errorf("pgup did not scroll the transcript behind the modal: %d → %d", before, m.vp.YOffset())
	}
}

// The two tests above cover pgup and the permission modal. These cover the rest: all six scroll
// keys, and the QUESTION modal, which was not exercised at all.
//
// The stake is not cosmetic. The prompt in front of the user is often a destructive command, and
// the question modal answers to 1-9, tab and enter — a scroll key mistaken for a choice would
// approve or answer something the user was only trying to read the context of.
var scrollKeys = []tea.KeyPressMsg{
	{Code: tea.KeyPgUp}, {Code: tea.KeyPgDown},
	{Code: 'u', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl},
	{Code: tea.KeyUp, Mod: tea.ModShift}, {Code: tea.KeyDown, Mod: tea.ModShift},
}

func scrollableModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.vp.SetWidth(40)
	m.vp.SetHeight(3)
	m.vp.SetContent(strings.Repeat("line\n", 50))
	m.vp.GotoBottom()
	return m
}

func TestEveryScrollKeyLeavesThePermissionPromptStanding(t *testing.T) {
	for _, k := range scrollKeys {
		m := scrollableModel(t)
		m.perm = &permReq{callID: "c1", name: "bash", args: `{"command":"rm -rf build"}`, reason: "destructive"}
		if _, handled := m.handleKey(k); !handled {
			t.Errorf("%v was not handled while the prompt was up", k)
		}
		if m.perm == nil {
			t.Errorf("%v answered the permission prompt", k)
		}
	}
}

func TestEveryScrollKeyLeavesTheQuestionStanding(t *testing.T) {
	for _, k := range scrollKeys {
		m := scrollableModel(t)
		m.quest = &questReq{callID: "q1", question: "which branch?", options: []string{"main", "develop"}}
		sel := m.quest.sel
		if _, handled := m.handleKey(k); !handled {
			t.Errorf("%v was not handled while the question was up", k)
		}
		if m.quest == nil {
			t.Errorf("%v answered the question", k)
		} else if m.quest.sel != sel {
			t.Errorf("%v moved the selection %d → %d", k, sel, m.quest.sel)
		}
	}
}

// …and the question still answers to its own keys, so this is a live scroll key rather than a
// dead modal.
func TestAQuestionStillAnswersToItsOwnKeys(t *testing.T) {
	m := scrollableModel(t)
	m.quest = &questReq{callID: "q1", question: "which branch?", options: []string{"main", "develop"}}
	if _, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab}); !handled {
		t.Fatal("tab was not handled")
	}
	if m.quest == nil || m.quest.sel != 1 {
		t.Fatalf("tab did not move the selection: %+v", m.quest)
	}
}

// Every scroll key is consumed; nothing else is. A key swallowed by mistake is a key the modal
// never sees.
func TestScrollTranscriptKeyConsumesExactlyTheScrollKeys(t *testing.T) {
	m := scrollableModel(t)
	for _, k := range []string{"pgup", "pgdown", "ctrl+u", "ctrl+d", "shift+up", "shift+down"} {
		if !m.scrollTranscriptKey(k) {
			t.Errorf("%q is a scroll key and was not consumed", k)
		}
	}
	for _, k := range []string{"enter", "esc", "a", "1", "tab", "up", "down", ""} {
		if m.scrollTranscriptKey(k) {
			t.Errorf("%q is not a scroll key and was swallowed", k)
		}
	}
}

// The offset stays inside the content however hard the keys are leaned on. The random walk asserts
// this for the wheel; the keyboard is a different entry point into the same viewport.
func TestScrollKeysStayInsideTheContent(t *testing.T) {
	m := scrollableModel(t)
	for i := 0; i < 40; i++ {
		m.scrollTranscriptKey("pgup")
		m.scrollTranscriptKey("shift+up")
	}
	if off := m.vp.YOffset(); off != 0 {
		t.Errorf("scrolling up past the top left offset %d", off)
	}
	for i := 0; i < 40; i++ {
		m.scrollTranscriptKey("pgdown")
		m.scrollTranscriptKey("shift+down")
	}
	if off, total := m.vp.YOffset(), m.vp.TotalLineCount(); off < 0 || off > total {
		t.Errorf("offset %d outside %d lines of content", off, total)
	}
}
