package tui

import "testing"

// A finished pane caches its rendered overview lines (per width); a running pane
// does not (its content is still changing).
func TestPaneTailCachesWhenDone(t *testing.T) {
	applyTheme(true)
	m := &Model{roleColor: map[string]int{}}
	p := &agentPane{role: "explore", blocks: []block{
		{kind: blockToolCall, name: "report", args: `{"status":"done"}`, done: true, ok: true},
	}}

	// Running: no cache.
	_ = m.paneTail(p, 60, 10)
	if p.tailLines != nil {
		t.Fatal("running pane should not be cached")
	}

	// Done: cached for this width.
	p.done = true
	out := m.paneTail(p, 60, 10)
	if p.tailLines == nil || p.tailCacheW != 60 {
		t.Fatalf("done pane should cache (lines=%v w=%d)", p.tailLines, p.tailCacheW)
	}
	if got := m.paneTail(p, 60, 10); got != out {
		t.Errorf("cached render differs: %q vs %q", out, got)
	}

	// Width change invalidates the cache (re-rendered at the new width).
	_ = m.paneTail(p, 40, 10)
	if p.tailCacheW != 40 {
		t.Errorf("width change should refresh cache width, got %d", p.tailCacheW)
	}
}
