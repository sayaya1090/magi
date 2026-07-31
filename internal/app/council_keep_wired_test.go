package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// recordingCouncil captures the request the app actually sends.
type recordingCouncil struct {
	got port.DeliberationRequest
}

func (c *recordingCouncil) Deliberate(_ context.Context, r port.DeliberationRequest) (council.Deliberation, error) {
	c.got = r
	return council.Deliberation{Decision: council.Done}, nil
}

// The whole keep path was built and unreachable. The adapter asks members for it, parses it, the
// TUI renders it and renderCouncilAdvice surfaces it — and nothing set the REQUEST field, so
// members were never asked and the advice was always empty. It had been wired through four
// deliberation phases once; the phases came out and the one surviving call site did not carry it.
//
// This asserts on the request the app sends, which is the seam that was broken. Asserting on the
// rendered advice would pass just as happily with the flag never set.
func TestTheCouncilIsAskedWhatToKeep(t *testing.T) {
	fc := &recordingCouncil{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()

	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "is this done?", false); err != nil {
		t.Fatal(err)
	}
	if !fc.got.Keep {
		t.Error("the members were not asked what to keep, so the advice can only ever list faults")
	}
	// The flag is the A/B knob the comment advertises — off means off, not ignored.
	t.Setenv("MAGI_COUNCIL_KEEP", "0")
	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "again?", false); err != nil {
		t.Fatal(err)
	}
	if fc.got.Keep {
		t.Error("MAGI_COUNCIL_KEEP=0 must restore fix-only feedback")
	}
}

// …and what they answer reaches the agent. The renderer has a branch for it that nothing could
// reach while the request field was false.
func TestWhatTheCouncilSaysToKeepReachesTheAgent(t *testing.T) {
	d := council.Deliberation{
		Decision: council.Continue,
		Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Feedback: "the parser is wrong"}},
		Keep:     "the retry loop is correct — do not revert it",
	}
	out := renderCouncilAdvice(d)
	if !strings.Contains(out, "do not revert it") {
		t.Errorf("the keep advice is not in what the agent reads:\n%s", out)
	}
}
