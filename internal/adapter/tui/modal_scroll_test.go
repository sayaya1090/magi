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
