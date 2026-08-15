package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// countingCouncil records how many times the members were actually polled.
type countingCouncil struct {
	calls    int
	decision council.Decision
}

func (c *countingCouncil) Deliberate(_ context.Context, _ port.DeliberationRequest) (council.Deliberation, error) {
	c.calls++
	return council.Deliberation{Decision: c.decision}, nil
}

// Re-declaring completion with nothing changed does not spend a fresh fan-out. The first council
// rejects; a second identical declaration (same report, same edits, no work in between) is
// short-circuited to the same rejection without polling the members again — the "nine councils on
// one unchanged sentence" waste.
func TestARepeatedIdenticalCouncilDoesNotFanOutAgain(t *testing.T) {
	fc := &countingCouncil{decision: council.Continue}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()

	// First two identical declarations both run — the second lets the members see their own prior
	// objection fed back. From the third on, the answer will not move, so it is short-circuited.
	for i := 0; i < 4; i++ {
		if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, "", true); err != nil {
			t.Fatal(err)
		}
	}
	if fc.calls != 2 {
		t.Errorf("four identical declarations polled the council %d times, want 2 (first repeat runs, rest short-circuit)", fc.calls)
	}
}
