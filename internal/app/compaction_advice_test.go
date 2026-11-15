package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Compaction is generic: it knows nothing of what an extension keeps. An extension that does keep
// something (a memory plugin, say) registers a compaction advisor, and what it says is handed to the
// summariser with the fold and condense prompts. A failing advisor never stops the fold.
type fixedAdvisor struct {
	text string
	got  port.CompactionQuery
}

func (f *fixedAdvisor) AdviseCompaction(_ context.Context, q port.CompactionQuery) (string, error) {
	f.got = q
	return f.text, nil
}

type failingAdvisor struct{}

func (failingAdvisor) AdviseCompaction(context.Context, port.CompactionQuery) (string, error) {
	return "", errors.New("advisor down")
}

func TestAnExtensionAdvisesTheFold(t *testing.T) {
	llm := &usageLLM{text: "brief"}
	a, sid := tiersApp(t, llm, 40000, Config{})
	adv := &fixedAdvisor{text: "Do not restate the entries listed here."}
	a.RegisterCompactionAdvisor(failingAdvisor{})
	a.RegisterCompactionAdvisor(adv)
	for i := 1; i <= 10; i++ {
		spoke(t, a, sid, fmt.Sprintf("a%d", i), "working")
	}
	if !foldNow(t, a, sid) {
		t.Fatal("a failing advisor stopped the fold")
	}
	sys := llm.lastSys()
	if !strings.Contains(sys, "Do not restate the entries listed here.") {
		t.Errorf("the advisor's guidance did not reach the summariser: %q", sys)
	}
	if !strings.Contains(sys, "Request") {
		t.Error("the fold prompt itself was lost")
	}
	if adv.got.SessionID != sid {
		t.Errorf("the advisor was asked about %q, want %q", adv.got.SessionID, sid)
	}
}

func TestAnExtensionAdvisesTheCondense(t *testing.T) {
	llm := &usageLLM{text: "short"}
	a, sid := tiersApp(t, llm, 40000, Config{})
	a.RegisterCompactionAdvisor(&fixedAdvisor{text: "Point at kept entries in one line."})
	ctx := context.Background()
	a.condenseBrief(ctx, AgentSpec{Name: "default"}, a.sessionInfo(ctx, sid), strings.Repeat("brief line\n", 200), 100)
	if sys := llm.lastSys(); !strings.Contains(sys, "Point at kept entries in one line.") {
		t.Errorf("the advisor's guidance did not reach the condense: %q", sys)
	}
}
