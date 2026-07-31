package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// ask_user blocks on the person. There is one human and one modal slot, so two of them in flight at
// once means the second question replaces the first on screen — the first prompt vanishes and its
// call waits on an answer nobody can give, with nothing on screen saying it is there. Read-only is
// not the same as parallel-safe when the thing being read is a human.
func TestAskUserIsNotRunConcurrently(t *testing.T) {
	a := &App{cfg: Config{}}
	a.cfg = a.cfg.withDefaults()

	calls := func(names ...string) []*session.ToolCall {
		out := make([]*session.ToolCall, 0, len(names))
		for _, n := range names {
			out = append(out, &session.ToolCall{Name: n})
		}
		return out
	}
	if a.allParallelSafe(calls("read", "grep")) != true {
		t.Error("two reads should still batch")
	}
	if a.allParallelSafe(calls("read", "ask_user")) {
		t.Error("a batch containing ask_user must run sequentially")
	}
	if a.allParallelSafe(calls("ask_user", "ask_user")) {
		t.Error("two questions must not be raised at once — the second would replace the first")
	}
	// The pre-existing exclusions still hold.
	if a.allParallelSafe(calls("read", "write")) {
		t.Error("a file modifier must serialize")
	}
	if a.allParallelSafe(calls("read", "bash")) {
		t.Error("a permissioned tool must serialize")
	}
}
