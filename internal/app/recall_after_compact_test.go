package app

import (
	"strings"
	"testing"
)

// A recall's output is ordinary conversation once it lands, so the next compaction can summarize it
// away — and compact.go then re-indexes that same region into recallable topics and tells the agent
// "recall_context can re-open the detail by topic". Refusing the invited call with "use what was
// returned earlier" points the agent at content magi had just removed. Observed in the field
// (qemu-alpine-ssh, 2026-07-31): topic "setup_alpine.py" recalled 4 messages, then refused 48
// events later.
func TestATopicMayBeRecalledAgainAfterItsContentWasCompactedAway(t *testing.T) {
	g := newRunGuard(nil)

	ok, why := g.allowRecall("setup_alpine.py")
	if !ok {
		t.Fatalf("first recall must be allowed: %s", why)
	}
	if ok, why := g.allowRecall("setup_alpine.py"); ok {
		t.Error("a duplicate recall inside one context IS a duplicate — the content is still there")
	} else if !strings.Contains(why, "already recalled") {
		t.Errorf("unexpected refusal: %q", why)
	}

	g.forgetRecalledTopics() // a compaction just shed it again

	if ok, why := g.allowRecall("setup_alpine.py"); !ok {
		t.Errorf("after a compaction the topic is gone again, so asking for it is not a duplicate: %s", why)
	}
}

// The budget is what bounds re-inflation within a turn, and a compaction does not buy a fresh
// allowance — it only makes one more request legitimate. A reset count would let recall→compact→
// recall spin, which is the loop the budget exists to stop.
func TestCompactionDoesNotRefillTheRecallBudget(t *testing.T) {
	g := newRunGuard(nil)
	for i := 0; i < recallBudget; i++ {
		if ok, why := g.allowRecall(string(rune('a' + i))); !ok {
			t.Fatalf("topic %d must fit in the budget: %s", i, why)
		}
	}
	g.forgetRecalledTopics()
	ok, why := g.allowRecall("anything")
	if ok {
		t.Error("the per-turn budget must survive a compaction")
	}
	if !strings.Contains(why, "budget") {
		t.Errorf("the refusal must name the budget, not the topic ledger: %q", why)
	}
}
