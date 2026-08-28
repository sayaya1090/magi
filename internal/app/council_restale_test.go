package app

import (
	"context"
	"strings"
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
		if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, 0, "", true); err != nil {
			t.Fatal(err)
		}
	}
	if fc.calls != 2 {
		t.Errorf("four identical declarations polled the council %d times, want 2 (first repeat runs, rest short-circuit)", fc.calls)
	}
}

// After a redirect interjection re-anchors the goal, the council judges the NEW task — not the
// abandoned original. The redirect masks its own prompt from the transcript view the council
// recomputes from, so without the live-task override the council fell back to the original goal
// and vetoed completion forever (a livelock observed live).
func TestCouncilJudgesTheRedirectedTask(t *testing.T) {
	fc := &recordingCouncil{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()

	a.setLiveTurnTask(sid, "create REDIRECTED.txt with the word DONE")
	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, 0, "", true); err != nil {
		t.Fatal(err)
	}
	if fc.got.Task != "create REDIRECTED.txt with the word DONE" {
		t.Errorf("the council judged %q, not the re-anchored redirect goal", fc.got.Task)
	}
}

func (c *countingCouncil) Advise(ctx context.Context, req port.AdviceRequest) (string, error) {
	return "yes", nil
}

// The short-circuit above must NOT fire when the turn did real work between the declarations.
//
// Its three-way comparison used to be report + changes, and `changes` is not a fact about the work:
// it is the evidence block CLIPPED to councilDiffCap so the members can read it, laid out in
// first-seen order. Edits to a file sitting past the clip therefore leave the string byte-identical
// — the premise this test measures before it asserts anything — and the agent was answered "no new
// edits, no new result" for a minute of editing. epoch is the guard's mutation count and says what
// actually happened.
func TestADeclarationAfterRealWorkStillFansOut(t *testing.T) {
	// Two different change sets whose CLIPPED rendering is the same. The first file alone overruns
	// the budget, so the second — the one that differs — never survives into what is compared.
	// Each is under councilFileFullCap, so both ride in full and together overrun councilDiffCap.
	bulk := []fileChange{
		{path: "a.go", after: strings.Repeat("a line of perfectly ordinary code\n", 100)},
		{path: "b.go", after: strings.Repeat("another line of ordinary code\n", 100)},
	}
	cs1 := append(append([]fileChange{}, bulk...), fileChange{path: "tail.go", after: "the first version\n"})
	cs2 := append(append([]fileChange{}, bulk...), fileChange{path: "tail.go", after: "a different version entirely\n"})
	clip := func(cs []fileChange) string {
		return truncateForCouncil(buildCouncilChanges(cs), councilDiffCap)
	}
	if clip(cs1) != clip(cs2) {
		t.Fatalf("premise gone: the clip no longer collapses these two change sets, so this test "+
			"is not exercising the defect (lens %d vs %d bytes)", len(clip(cs1)), len(clip(cs2)))
	}

	fc := &countingCouncil{decision: council.Continue}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()

	// Three declarations with nothing changing: two fan out, the third is short-circuited.
	for i := 0; i < 3; i++ {
		if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), cs1, 1, "", true); err != nil {
			t.Fatal(err)
		}
	}
	if fc.calls != 2 {
		t.Fatalf("the unchanged repeat polled the council %d times, want 2", fc.calls)
	}

	// Now the agent edits and declares again. Same words, same clipped evidence, different work.
	out, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), cs2, 2, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if fc.calls != 3 {
		t.Errorf("a declaration made AFTER real work was short-circuited (council polled %d times, "+
			"want 3): the clipped evidence was read as a fact about the work", fc.calls)
	}
	if strings.Contains(out, "no new edits") {
		t.Errorf("the agent was told it made no new edits after editing:\n%s", out)
	}
}
