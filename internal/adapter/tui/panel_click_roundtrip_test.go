package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/session"
)

// Every subagent row in the post-it records the screen row it was drawn on, and a click on that
// row opens THAT subagent. The rows are budgeted to the panel's inner width on the stated
// assumption that they never wrap — "a wrapped row would shift every later panelY and break
// clicks" — and the splitter can drag the panel down to 24 columns, where a long role sits
// beside a long status.
//
// Checked against the DRAWN box, not against the same field the click handler reads: mapping a
// click back through panelY only proves the lookup agrees with itself, and it agrees just as
// happily when every row is recorded one line off (measured: shifting panelY by one leaves that
// round trip entirely green).
func TestPanelRowIsDrawnWhereItIsRecorded(t *testing.T) {
	roles := []string{"explore", "coder", "tester", "reviewer", "planner", "documentation-writer"}
	for _, w := range []int{24, 30, 40, 64} {
		for _, h := range []int{24, 40} {
			t.Run(fmt.Sprintf("w%d_h%d", w, h), func(t *testing.T) {
				mm := newTestModel(t)
				m := &mm
				m.width, m.height = 160, h
				m.ready = true
				m.panelW = w
				for i, r := range roles {
					m.panes = append(m.panes, &agentPane{
						sid: session.SessionID(fmt.Sprintf("s%d", i)), role: r, sub: i,
						started: time.Now().Add(-time.Duration(i+1) * time.Minute),
					})
				}

				box, top, left, ok := m.floatPanel()
				if !ok {
					t.Fatalf("the panel should be on screen at panelW=%d h=%d", w, h)
				}
				rows := strings.Split(box, "\n")
				checked := 0
				for i, p := range m.panes {
					y := p.panelY - top // the box's own row index
					if p.panelY <= 0 || y < 0 || y >= len(rows) {
						continue // clipped away: it draws nothing to click
					}
					drawn := ansi.Strip(rows[y])
					// The label is budgeted and may be shortened, so match the longest prefix
					// that survives at this width rather than the whole role.
					head := p.role
					for len(head) > 3 && !strings.Contains(drawn, head) {
						head = head[:len(head)-1]
					}
					checked++
					if !strings.Contains(drawn, head) {
						t.Errorf("pane %d (%s) is recorded at y=%d, where the panel draws\n  %q",
							i, p.role, p.panelY, drawn)
						continue
					}
					// And a click there must reach that same pane.
					m.zoom, m.zoomPane, m.focusPane = false, nil, -1
					if m.handlePanelClick(left+2, p.panelY); m.focusPane != i {
						t.Errorf("clicking pane %d (%s) at y=%d focused %d", i, p.role, p.panelY, m.focusPane)
					}
				}
				if checked == 0 {
					t.Fatalf("no row was checked at panelW=%d h=%d — the case proves nothing", w, h)
				}
			})
		}
	}
}
