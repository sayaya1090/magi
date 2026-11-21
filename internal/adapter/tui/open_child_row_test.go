package tui

import (
	"reflect"
	"strings"
	"testing"
)

// A call that ran a child names it on its result; the call's own row is the way into it, as a
// subagent row in the panel is. A click on the head opens the child: the pane the strip already
// has for it, or a pinned one read from its session when the strip has none (a past turn).
func TestAToolRowThatRanAChildOpensIt(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 120, 40
	call := block{kind: blockToolCall, name: "dummy_plug", args: `{"goal":"count docs"}`, callID: "c1"}
	out := foldToolResultInto([]block{{kind: blockUser, text: "go"}, call}, "c1", "35 docs\n\n[child s_kid01, 2 tool call(s)]", true, false)
	if !reflect.DeepEqual(out[1].children, []string{"s_kid01"}) {
		t.Fatalf("the folded result must carry the child it names: %v", out[1].children)
	}
	plain := block{kind: blockToolCall, name: "read", args: `{"path":"a"}`, callID: "c2"}
	out = foldToolResultInto(append(out, plain), "c2", "hello", true, false)
	m.blocks = out
	view := ansiSeq.ReplaceAllString(m.transcript(), "") // records where each block starts
	if len(m.blockLineStart) < 3 {
		t.Fatalf("no line starts recorded: %v", m.blockLineStart)
	}
	if !strings.Contains(view, "child s_kid01") {
		t.Fatalf("the row must say which child it opens:\n%s", view)
	}
	if m.openChildAt(m.blockLineStart[2]) {
		t.Fatal("a call that ran no child opens nothing")
	}

	// The strip already has the child: its pane is opened.
	done := &agentPane{sid: "s_kid01", role: "dummy_plug", done: true}
	m.doneRoster = []*agentPane{done}
	if !m.openChildAt(m.blockLineStart[1]) || !m.zoom || m.viewedPane() != done {
		t.Fatalf("a click on the head must open the strip's pane for the child (zoom=%v)", m.zoom)
	}
	m.exitZoom()

	// A past turn: no pane anywhere. One is made for the child and pinned.
	m.doneRoster = nil
	if !m.openChildAt(m.blockLineStart[1]+1) || !m.zoom {
		t.Fatal("a click on the child line must open the child even when no pane shows it")
	}
	if p := m.viewedPane(); p == nil || string(p.sid) != "s_kid01" || !p.done {
		t.Fatalf("the detail must show the named child as a finished pane: %+v", p)
	}
}
