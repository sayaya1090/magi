package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A delegate is "not done" on a spawn error, an empty result, or a worker report whose leading
// STATUS is BLOCKED/FAILED — so an unmet acceptance item re-plans instead of being marked complete.
func TestDelegateNotDone(t *testing.T) {
	nd := func(r port.SpawnResult) bool { return delegateNotDone(r, r.Text) }

	if !nd(port.SpawnResult{Err: "recursion limit"}) {
		t.Error("spawn error → not done")
	}
	if !nd(port.SpawnResult{Text: ""}) {
		t.Error("empty result → not done")
	}
	if !nd(port.SpawnResult{Text: "STATUS: BLOCKED\ncannot find the vendored archive"}) {
		t.Error("BLOCKED report → not done")
	}
	if !nd(port.SpawnResult{Text: "STATUS: FAILED\ntests do not pass"}) {
		t.Error("FAILED report → not done")
	}
	if nd(port.SpawnResult{Text: "STATUS: DONE\nall checks pass"}) {
		t.Error("DONE report → done")
	}
	if nd(port.SpawnResult{Text: "wrote server.py and verified it"}) {
		t.Error("plain non-empty result → done")
	}
}
