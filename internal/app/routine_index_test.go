package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// routineDoc is a minimal reader routine with the given one-line description.
func routineDoc(desc string) []byte {
	return []byte("---\nkind: routine\ndescription: " + desc + "\ntools: read\n---\n\n```lua\nprint(call_tool(\"read\", {path=\"notes.txt\"}))\n```\n")
}

// routineIndex returns the always-on routine list the plug-in adds to this turn's context ("" if none).
func routineIndex(t *testing.T, r *routineRig, prompt string) string {
	t.Helper()
	for _, p := range r.a.contextProviders {
		hints, err := p.Provide(context.Background(), port.ContextQuery{SessionID: r.sid, Workdir: r.s.Workdir, Prompt: prompt})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hints {
			if strings.Contains(h.Text, "# Saved routines") {
				return h.Text
			}
		}
	}
	return ""
}

// The model chooses a routine while it plans, so every active routine is listed on every turn,
// whatever the request says — and one saved mid-session shows up on the next turn, like a skill.
func TestRoutineIndexListsActiveRoutinesEveryTurnIncludingNewOnes(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	first := routineIndex(t, r, "hello there")
	if !strings.Contains(first, "- readit: read a file and say so") {
		t.Fatalf("active routine not listed for an unrelated request:\n%s", first)
	}
	r.put(t, filepath.Join(".magi", "routines", "later", "routine.md"), routineDoc("count lines saved mid-session"))
	next := routineIndex(t, r, "something else")
	if !strings.Contains(next, "- later: count lines saved mid-session") || !strings.Contains(next, "- readit:") {
		t.Fatalf("routine saved mid-session missing from the next turn:\n%s", next)
	}
}

// The list stops at whichever comes first, 10 entries or 1.5KB, and points at routine_catalog for the rest.
func TestRoutineIndexStaysWithinTenLinesAndBudget(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	for i := range 14 {
		r.put(t, filepath.Join(".magi", "routines", fmt.Sprintf("r%02d", i), "routine.md"), routineDoc(strings.Repeat("long purpose ", 12)))
	}
	idx := routineIndex(t, r, "anything")
	lines := 0
	for _, l := range strings.Split(idx, "\n") {
		if strings.HasPrefix(l, "- ") {
			lines++
		}
	}
	if lines == 0 || lines > 10 || len(idx) > 1500 || !strings.Contains(idx, "routine_catalog") {
		t.Fatalf("lines=%d bytes=%d:\n%s", lines, len(idx), idx)
	}
	short := newRoutineRig(t, t.TempDir(), routineFile)
	for i := range 14 {
		short.put(t, filepath.Join(".magi", "routines", fmt.Sprintf("s%02d", i), "routine.md"), routineDoc("short"))
	}
	idx = routineIndex(t, short, "anything")
	lines = strings.Count(idx, "\n- ")
	if lines != 10 || !strings.Contains(idx, "routine_catalog") {
		t.Fatalf("short entries: want 10 lines, got %d:\n%s", lines, idx)
	}
}

// Idle maintenance takes a routine that was listed many times and never run out of the list;
// one the model ran stays. The dropped one still runs by name.
func TestIdleDropsListedButUnusedRoutinesFromTheIndex(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join(".magi", "routines", "unused", "routine.md"), routineDoc("never picked"))
	if out, _, failed := r.run(t, "u1", map[string]any{"file": "notes.txt"}, "allow"); failed {
		t.Fatal(out)
	}
	for i := range 20 {
		if idx := routineIndex(t, r, fmt.Sprintf("turn %d", i)); !strings.Contains(idx, "- unused:") {
			t.Fatalf("turn %d: not listed yet:\n%s", i, idx)
		}
	}
	r.fire("idle")
	idx := routineIndex(t, r, "after idle")
	if strings.Contains(idx, "- unused:") || !strings.Contains(idx, "- readit:") {
		t.Fatalf("idle should drop the unused routine and keep the adopted one:\n%s", idx)
	}
	if out, _, failed := r.call(t, "u2", "run_routine", map[string]any{"name": "unused"}, "allow"); failed {
		t.Fatalf("a dropped routine must still run by name: %s", out)
	}
}

// The provider runs again on every model step of a turn. Those repeats are one exposure: a single
// long turn must not count as twenty listings and drop a routine before it had a chance.
func TestRoutineIndexCountsOneExposurePerRequest(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join(".magi", "routines", "unused", "routine.md"), routineDoc("not picked yet"))
	for range 25 {
		if idx := routineIndex(t, r, "one long request"); !strings.Contains(idx, "- unused:") {
			t.Fatalf("not listed:\n%s", idx)
		}
	}
	r.fire("idle")
	if idx := routineIndex(t, r, "the next request"); !strings.Contains(idx, "- unused:") {
		t.Fatalf("25 steps of one request counted as 25 exposures and dropped the routine:\n%s", idx)
	}
}
