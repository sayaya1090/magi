package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// A finished pane stays visible through its own linger window, dims over the fade
// window, then is removed — keyed on the pane's OWN doneAt.
func TestPaneFadeOutLifecycle(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 40
	m.running = false
	m.focusPane = -1
	p := &agentPane{role: "explore", done: true}
	m.panes = []*agentPane{p}

	p.doneAt = time.Time{} // not recorded → no fade
	m.advancePaneFade()
	if p.fade != 0 || len(m.panes) != 1 {
		t.Fatalf("no doneAt → no fade, got fade=%v panes=%d", p.fade, len(m.panes))
	}

	p.doneAt = time.Now().Add(-paneFadeAfter / 2) // within linger → fully visible
	m.advancePaneFade()
	if p.fade != 0 || len(m.panes) != 1 {
		t.Fatalf("during linger → fade 0, still shown, got fade=%v panes=%d", p.fade, len(m.panes))
	}

	p.doneAt = time.Now().Add(-(paneFadeAfter + paneFadeDur/2)) // mid-fade → partial
	m.advancePaneFade()
	if !(p.fade > 0 && p.fade < 1) || len(m.panes) != 1 {
		t.Fatalf("mid-fade → 0<fade<1, still shown, got fade=%v panes=%d", p.fade, len(m.panes))
	}

	p.doneAt = time.Now().Add(-(paneFadeAfter + 2*paneFadeDur)) // past the fade → removed
	m.advancePaneFade()
	if len(m.panes) != 0 {
		t.Fatalf("after its fade window the finished pane should be removed, got %d", len(m.panes))
	}
	if _, _, _, total := m.paneLayout(); total != 0 {
		t.Fatalf("cleared block should reserve 0 rows, got %d", total)
	}
}

// A pane that fades out leaves the inline strip but PERSISTS in the right panel's
// roster (the panel intentionally does not fade out — it's a record of what ran).
func TestFadedPaneStaysInPanelRoster(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 40
	m.running = false
	m.focusPane = -1
	p := &agentPane{role: "explore", task: "scout", done: true,
		doneAt: time.Now().Add(-(paneFadeAfter + 2*paneFadeDur))}
	m.panes = []*agentPane{p}

	m.advancePaneFade()
	if len(m.panes) != 0 {
		t.Fatalf("faded pane should leave the inline list, got %d", len(m.panes))
	}
	if len(m.doneRoster) != 1 || m.doneRoster[0] != p {
		t.Fatalf("faded pane should persist in the panel roster, got %d", len(m.doneRoster))
	}
	if panel := m.statusPanel(2); !strings.Contains(panel, "explore") {
		t.Fatalf("panel should keep listing the finished subagent:\n%s", panel)
	}
}

// The right panel lists subagents in spawn order even after one fades into the
// roster — a finished subagent keeps its position instead of jumping to the bottom.
func TestPanelKeepsSubagentOrderAfterFade(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 40
	pa := &agentPane{role: "aaa", sub: 1, done: true}
	pb := &agentPane{role: "bbb", sub: 2, done: true} // the middle one faded out
	pc := &agentPane{role: "ccc", sub: 3}
	m.panes = []*agentPane{pa, pc}  // active (out of spawn order in the slice)
	m.doneRoster = []*agentPane{pb} // faded
	panel := m.statusPanel(2)
	ia, ib, ic := strings.Index(panel, "aaa"), strings.Index(panel, "bbb"), strings.Index(panel, "ccc")
	if !(ia >= 0 && ib >= 0 && ic >= 0 && ia < ib && ib < ic) {
		t.Fatalf("panel should keep spawn order aaa<bbb<ccc, got %d,%d,%d:\n%s", ia, ib, ic, panel)
	}
}

// Each finished pane fades on its OWN clock: an early-finished pane is removed while
// a sibling is still running (it doesn't wait for the others or the turn).
func TestPaneFadeIsPerPane(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 40
	m.running = true // turn still in progress; one explorer done, one still running
	m.focusPane = -1
	doneEarly := &agentPane{role: "explore", task: "scout", done: true,
		doneAt: time.Now().Add(-(paneFadeAfter + 2*paneFadeDur))} // its fade window has passed
	stillRunning := &agentPane{role: "explore", task: "reader"} // not done
	m.panes = []*agentPane{doneEarly, stillRunning}

	m.advancePaneFade()
	if len(m.panes) != 1 || m.panes[0] != stillRunning {
		t.Fatalf("the early-finished pane should be removed while the running one stays, got %d panes", len(m.panes))
	}
}

// End-to-end: a render-tick message (the heartbeat) drives a pane's fade to removal
// through Update + View — proving the whole mechanism is wired.
func TestRenderTickDrivesFadeToRemoval(t *testing.T) {
	base := newTestModel(t)
	mm, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model := mm.(Model)
	m := &model
	m.running = false
	m.focusPane = -1
	m.panes = []*agentPane{{role: "explore", task: "find docs", done: true,
		doneAt: time.Now().Add(-(paneFadeAfter + 2*paneFadeDur))}}
	m.refresh()
	if !strings.Contains(m.View().Content, "find docs") {
		t.Fatal("pane should be visible before the tick advances the fade")
	}
	updated, _ := m.Update(renderTickMsg{}) // one heartbeat
	m2 := updated.(Model)
	if len(m2.panes) != 0 {
		t.Fatal("a render tick past the fade window should remove the finished pane")
	}
	if strings.Contains(m2.View().Content, "find docs") {
		t.Fatal("cleared pane should no longer appear in the view")
	}
}

// The fade pauses while a pane is focused, so it never vanishes mid-read.
func TestPaneFadePausesWhileFocused(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 40
	m.running = false
	m.panes = []*agentPane{{role: "explore", done: true,
		doneAt: time.Now().Add(-(paneFadeAfter + 2*paneFadeDur))}} // well past the window
	m.focusPane = 0 // but it's focused
	m.advancePaneFade()
	if m.panes[0].fade != 0 || len(m.panes) != 1 {
		t.Fatalf("focused pane should pause its fade (kept, undimmed), got fade=%v panes=%d", m.panes[0].fade, len(m.panes))
	}
}

// The pane block must never push the input off-screen: chrome + a minimum
// viewport always fits the height, and the rendered pane height matches the
// reserved block (no reserve/render drift). Overflow folds into "+N more".
func TestPaneLayoutKeepsInputOnScreen(t *testing.T) {
	for _, running := range []bool{true, false} {
		mm := newTestModel(t)
		m := &mm
		m.width, m.height = 80, 24
		m.running = running
		for i := 0; i < 30; i++ {
			m.panes = append(m.panes, &agentPane{role: "explore"})
		}
		if got := m.chromeHeight() + minViewport; got > m.height {
			t.Fatalf("running=%v: chrome+minViewport = %d > height %d (input pushed off-screen)", running, got, m.height)
		}
		pv := m.renderPanes(m.width, 5)
		if h := lipgloss.Height(pv); h != m.panesBlockHeight() {
			t.Fatalf("running=%v: rendered panes %d != reserved panesBlockHeight %d (drift)", running, h, m.panesBlockHeight())
		}
		if !strings.Contains(pv, "↓") {
			t.Errorf("running=%v: 30 agents on a 24-row screen should fold into a '+N more' line:\n%s", running, pv)
		}
	}
}

// paneScroll windows which panes are on screen and is clamped to the valid range
// so it can never scroll past the last pane or above the first.
func TestPaneScrollWindowsAndClamps(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.running = true
	for i := 0; i < 30; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore", task: fmt.Sprintf("agent-%d", i)})
	}
	nShown, _, _, _ := m.paneLayout()
	maxOff := len(m.panes) - nShown

	// Scrolling down past the end clamps to maxOff; the last pane becomes visible.
	m.paneScroll = 999
	pv := m.renderPanes(m.width, 5)
	if m.paneScroll != maxOff {
		t.Fatalf("over-scroll should clamp to %d, got %d", maxOff, m.paneScroll)
	}
	if !strings.Contains(pv, fmt.Sprintf("agent-%d", len(m.panes)-1)) {
		t.Errorf("max scroll should reveal the last pane:\n%s", pv)
	}
	// The no-drift invariant holds at a non-zero offset too: render height == reserve.
	if h := lipgloss.Height(pv); h != m.panesBlockHeight() {
		t.Fatalf("scrolled render height %d != reserved %d (drift at offset)", h, m.panesBlockHeight())
	}
	// Scrolling up past the top clamps to 0; the first pane is visible again.
	m.paneScroll = -5
	pv = m.renderPanes(m.width, 5)
	if m.paneScroll != 0 {
		t.Fatalf("under-scroll should clamp to 0, got %d", m.paneScroll)
	}
	if !strings.Contains(pv, "agent-0") {
		t.Errorf("zero scroll should show the first pane:\n%s", pv)
	}
}

// The pane block is bounded to ~half the screen so a burst of agents can't crowd
// out the transcript; the rest overflow into the scroll window.
func TestPaneBlockCappedToHalfScreen(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 40
	m.running = true
	for i := 0; i < 12; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore"})
	}
	_, _, more, total := m.paneLayout()
	if total > m.height/2 {
		t.Fatalf("pane block %d rows exceeds half the %d-row screen (crowds transcript)", total, m.height)
	}
	if more == 0 {
		t.Fatalf("with 12 agents on a capped block, the excess should overflow to the scroll window (more>0)")
	}
	// The transcript keeps well more than the bare minimum once the block is capped.
	if vp := m.height - m.baseChromeHeight() - total; vp < minViewport {
		t.Fatalf("viewport %d below minimum %d", vp, minViewport)
	}
}

// In a focused pane list, ↓ moves the selection and the window follows it so the
// focused pane can never scroll out of view.
func TestPaneKeyFocusFollowsWindow(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.running = true
	for i := 0; i < 30; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore", task: fmt.Sprintf("agent-%d", i)})
	}
	m.focusPane = 0
	nShown, _, _, _ := m.paneLayout()
	// Press ↓ past the bottom of the first window; focus and window advance together.
	for i := 0; i < nShown+2; i++ {
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.focusPane != nShown+2 {
		t.Fatalf("↓ should advance focus to %d, got %d", nShown+2, m.focusPane)
	}
	if m.focusPane < m.paneScroll || m.focusPane >= m.paneScroll+nShown {
		t.Fatalf("focused pane %d scrolled out of window [%d,%d)", m.focusPane, m.paneScroll, m.paneScroll+nShown)
	}
}

// The wheel routes to the pane list when a pane is focused, regardless of where the
// cursor sits — so scrolling stops "competing" with the transcript under the cursor.
func TestWheelRoutesToFocusedPaneList(t *testing.T) {
	base := newTestModel(t)
	mm, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model := mm.(Model)
	m := &model
	m.running = true
	for i := 0; i < 20; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore"})
	}
	m.refresh()
	if m.panesBlockHeight() == 0 {
		t.Fatal("expected a non-empty pane block")
	}
	transcriptY := 3 // inside the transcript region, above the pane block

	// Unfocused + cursor over the transcript → the wheel scrolls the transcript, not panes.
	m.focusPane = -1
	m.paneScroll = 0
	m.handleMouse(tea.MouseWheelMsg{Y: transcriptY, Button: tea.MouseWheelDown})
	if m.paneScroll != 0 {
		t.Fatalf("unfocused wheel over the transcript must not scroll panes, paneScroll=%d", m.paneScroll)
	}

	// Focused → the same wheel scrolls the pane list (focus, not cursor Y, decides).
	m.focusPane = 0
	m.handleMouse(tea.MouseWheelMsg{Y: transcriptY, Button: tea.MouseWheelDown})
	if m.paneScroll == 0 {
		t.Fatal("focused wheel should scroll the pane list regardless of cursor position")
	}
}

// Panes hidden behind "+N more" must have their hit-test rect cleared, so a click
// can't route to a pane that isn't on screen.
func TestPaneLayoutClearsHiddenRects(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 24
	m.running = true
	for i := 0; i < 30; i++ {
		m.panes = append(m.panes, &agentPane{role: "explore", y: 99, h: 5}) // stale rects
	}
	m.renderPanes(m.width, 5)
	nShown, _, _, _ := m.paneLayout()
	for i := nShown; i < len(m.panes); i++ {
		if m.panes[i].h != 0 || m.panes[i].y != 0 {
			t.Fatalf("hidden pane %d kept a stale rect (y=%d h=%d) → click could misroute", i, m.panes[i].y, m.panes[i].h)
		}
	}
}

// With few agents and a tall screen, all panes show and there's no "+N more".
func TestPaneLayoutShowsAllWhenRoomy(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 80, 60
	m.running = true
	m.panes = []*agentPane{{role: "a"}, {role: "b"}}
	nShown, _, more, _ := m.paneLayout()
	if nShown != 2 || more != 0 {
		t.Fatalf("roomy screen should show both panes with no overflow, got nShown=%d more=%d", nShown, more)
	}
	if m.chromeHeight()+minViewport > m.height {
		t.Fatalf("invariant broken even when roomy")
	}
}

// The input box must sit at the bottom: the rendered view fills the screen height
// even with a short transcript and several running panes (no floating gap).
func TestViewFillsHeightWithPanes(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = mm.(Model)
	m.running = true
	m.panes = []*agentPane{{role: "a"}, {role: "b"}, {role: "c"}}
	m.refresh()
	v := m.View()
	if h := lipgloss.Height(v.Content); h != 30 {
		t.Fatalf("view content height %d, want 30 — input box floats with empty space below when shorter", h)
	}
}
