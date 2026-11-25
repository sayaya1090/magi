package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// routineDoc is a minimal reader routine with the given one-line description.
func routineDoc(desc string) []byte {
	return []byte("---\nkind: routine\ndescription: " + desc + "\ntools: read\n---\n\n```lua\nprint(call_tool(\"read\", {path=\"notes.txt\"}))\n```\n")
}

// routineIndex returns the always-on routine list the plug-in adds to this turn's context ("" if none).
func routineIndex(t *testing.T, r *routineRig, prompt string) string {
	t.Helper()
	// A request arrives (the plug-in counts it and refreshes the routine skills), then the person's view
	// of what was chosen, in value order.
	r.fire("user_message", "text", prompt)
	if err := r.command(t, "routine-skills"); err != nil {
		t.Fatal(err)
	}
	text := r.logs.text()
	if i := strings.LastIndex(text, "# Saved routines"); i >= 0 {
		return text[i:]
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

// learnReaders repeats a grep→read stretch in two turns; respond names the model response that wrote each
// call, so the plug-in can count the model steps the stretch took. It returns the saved routine's name.
func learnReaders(t *testing.T, r *routineRig, respond func(turn, call int) string) string {
	t.Helper()
	r.file(t, "a1b2c3d4.txt", "a\n")
	r.file(t, "f9e8d7c6.txt", "f\n")
	for i, file := range []string{"a1b2c3d4.txt", "f9e8d7c6.txt"} {
		r.fire("user_message", "text", "open "+file)
		r.fire("tool_finished", "order_known", "true", "tool", "grep", "args", `{"path":"notes.txt","pattern":"hello"}`, "ok", "true", "response_id", respond(i, 0))
		r.fire("tool_finished", "order_known", "true", "tool", "read", "args", `{"path":"`+file+`"}`, "ok", "true", "response_id", respond(i, 1))
		r.fire("turn_finished")
	}
	m := regexp.MustCompile(`candidate (t[0-9a-f]{8})`).FindStringSubmatch(r.logs.text())
	if m == nil {
		t.Fatalf("no routine learned: %q", r.logs.text())
	}
	return "auto-" + m[1]
}

// A routine is worth the model steps it replaces. A stretch the model wrote in two responses saves one
// model call per run (two steps become one run_routine call), and the index says so.
func TestRoutineIndexStatesTheModelCallsARoutineSaves(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	name := learnReaders(t, r, func(turn, call int) string { return fmt.Sprintf("resp-%d-%d", turn, call) })
	if idx := routineIndex(t, r, "anything"); !strings.Contains(idx, "- "+name+":") || !strings.Contains(idx, "saves 1 model call per run)") {
		t.Fatalf("two responses, one run: want one saved call listed:\n%s", idx)
	}
}

// The same stretch written in one response saved nothing: the model already did it in one step. The
// routine stays on disk and runs by name, but is not worth a line in every turn's context.
func TestRoutineIndexLeavesOutRoutinesThatSaveNoModelCall(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	name := learnReaders(t, r, func(turn, _ int) string { return fmt.Sprintf("resp-%d", turn) })
	if _, err := os.Stat(filepath.Join(r.s.Workdir, ".magi", "routines", name, "routine.md")); err != nil {
		t.Fatalf("routine not saved: %v", err)
	}
	if idx := routineIndex(t, r, "anything"); strings.Contains(idx, "- "+name+":") || !strings.Contains(idx, "- readit:") {
		t.Fatalf("a routine saving no call was listed (or the hand-written one was lost):\n%s", idx)
	}
}

// A changed version is valued by the increment over the observed original: one more tool call in the
// script adds one model step, and the value is marked as an estimate.
func TestRoutineValueOfAChangedVersionIsTheIncrementOverTheOriginal(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	name := learnReaders(t, r, func(turn, call int) string { return fmt.Sprintf("resp-%d-%d", turn, call) })
	path := filepath.Join(".magi", "routines", name, "routine.md")
	body := r.skill(t, name)
	i := strings.Index(body, "```lua\n")
	if i < 0 {
		t.Fatalf("no script: %s", body)
	}
	i += len("```lua\n")
	r.put(t, path, []byte(body[:i]+"call_tool(\"read\", {path=\"notes.txt\"})\n"+body[i:]))
	if idx := routineIndex(t, r, "anything"); !strings.Contains(idx, "saves 2 model calls per run, estimated") {
		t.Fatalf("changed version: want the increment as an estimate:\n%s", idx)
	}
}

// The index is ordered by value, not by name: a routine that just ran successfully moves ahead of one
// that has not run yet.
func TestRoutineIndexOrdersByValueFromSuccessfulRuns(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	name := learnReaders(t, r, func(turn, call int) string { return fmt.Sprintf("resp-%d-%d", turn, call) })
	before := routineIndex(t, r, "anything")
	if strings.Index(before, "- "+name+":") > strings.Index(before, "- readit:") {
		t.Fatalf("with no runs the order should fall back to names:\n%s", before)
	}
	if out, _, failed := r.run(t, "v1", map[string]any{"file": "notes.txt"}, "allow"); failed {
		t.Fatal(out)
	}
	after := routineIndex(t, r, "another request")
	if strings.Index(after, "- readit:") > strings.Index(after, "- "+name+":") {
		t.Fatalf("a successful run should raise readit's value above the unrun routine:\n%s", after)
	}
}

// Each listing costs the tokens of its line, so a routine listed for a while and never run is worth less
// than one just added.
func TestRoutineValueChargesItsListings(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join(".magi", "routines", "aaa", "routine.md"), routineDoc("listed for a while"))
	for i := range 5 {
		routineIndex(t, r, fmt.Sprintf("request %d", i))
	}
	r.put(t, filepath.Join(".magi", "routines", "bbb", "routine.md"), routineDoc("just added"))
	idx := routineIndex(t, r, "one more")
	if strings.Index(idx, "- bbb:") > strings.Index(idx, "- aaa:") {
		t.Fatalf("listings cost nothing:\n%s", idx)
	}
}

// Idle maintenance drops by value, not by whether the routine was ever picked: one that was run but failed
// and kept being listed is worth less than nothing.
func TestIdleDropsByValueNotByAdoption(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	if _, _, failed := r.run(t, "f1", map[string]any{"file": "missing.txt"}, "allow"); !failed {
		t.Fatal("the run was meant to fail")
	}
	for i := range 20 {
		routineIndex(t, r, fmt.Sprintf("turn %d", i))
	}
	r.fire("idle")
	if idx := routineIndex(t, r, "after idle"); strings.Contains(idx, "- readit:") {
		t.Fatalf("a failed, much-listed routine kept its place:\n%s", idx)
	}
}

// learnStretch learns one reader stretch from two turns, each call in its own model response.
func learnStretch(t *testing.T, r *routineRig, tag string, reads int) {
	t.Helper()
	for i := range 2 {
		file := fmt.Sprintf("%s%d0f9e8d7.txt", tag, i)
		r.file(t, file, "x\n")
		r.fire("user_message", "text", "open "+file)
		r.fire("tool_finished", "order_known", "true", "tool", "grep", "args", `{"path":"notes.txt","pattern":"hello"}`, "ok", "true", "response_id", fmt.Sprintf("%s-%d-g", tag, i))
		for k := range reads {
			path := file
			if k > 0 {
				path = "notes.txt"
			}
			r.fire("tool_finished", "order_known", "true", "tool", "read", "args", `{"path":"`+path+`"}`, "ok", "true", "response_id", fmt.Sprintf("%s-%d-r%d", tag, i, k))
		}
		r.fire("turn_finished")
	}
}

// Overlapping routines are counted once: when one learned stretch is contained in another, only the one
// worth more (occurrences × saved calls) is listed. The other still runs by name.
func TestOverlappingRoutinesListOnlyTheOneWorthMore(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	learnStretch(t, r, "s", 1)
	learnStretch(t, r, "l", 2)
	idx := routineIndex(t, r, "anything")
	if !strings.Contains(idx, "grep>read>read (saves 2 model calls per run)") || strings.Contains(idx, "grep>read (saves 1") {
		t.Fatalf("want only the longer stretch listed:\n%s", idx)
	}
}

// A learned routine that has not run yet is valued by shrinking toward how often listed routines here are
// run, so it is not ranked as worthless next to a hand-written routine nobody runs.
func TestANewRoutineIsValuedByShrinkingTowardOthers(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join(".magi", "routines", "aaa", "routine.md"), routineDoc("hand-written, never run"))
	if out, _, failed := r.run(t, "ok1", map[string]any{"file": "notes.txt"}, "allow"); failed {
		t.Fatal(out)
	}
	name := learnReaders(t, r, func(turn, call int) string { return fmt.Sprintf("resp-%d-%d", turn, call) })
	idx := routineIndex(t, r, "anything")
	if strings.Index(idx, "- "+name+":") > strings.Index(idx, "- aaa:") {
		t.Fatalf("a new learned routine ranked as worthless:\n%s", idx)
	}
}

// learnSeq learns one reader stretch (tools in order, each call in its own model response) from two turns
// and returns the saved routine's name, found by its description.
func learnSeq(t *testing.T, r *routineRig, tag string, tools ...string) string {
	t.Helper()
	for i := range 2 {
		file := fmt.Sprintf("%s%d7c6b5a4.txt", tag, i)
		r.file(t, file, "x\n")
		r.fire("user_message", "text", "open "+file)
		for k, tool := range tools {
			args := `{"path":"` + file + `"}`
			if tool == "grep" {
				args = `{"path":"docs","pattern":"hello"}`
			}
			r.fire("tool_finished", "order_known", "true", "tool", tool, "args", args, "ok", "true", "response_id", fmt.Sprintf("%s-%d-%d", tag, i, k))
		}
		r.fire("turn_finished")
	}
	want := "description: Read-only sequence: " + strings.Join(tools, ">") + "\n"
	ents, _ := os.ReadDir(filepath.Join(r.s.Workdir, ".magi", "routines"))
	for _, e := range ents {
		if strings.Contains(r.skill(t, e.Name()), want) {
			return e.Name()
		}
	}
	t.Fatalf("%s not learned: %q", strings.Join(tools, ">"), r.logs.text())
	return ""
}

// A routine hidden by an overlapping one cannot hide a third: b contains both a and c, c outweighs b and b
// outweighs a. c hides b, and a, which overlaps only b, must stay listed.
func TestAHiddenRoutineDoesNotHideAnother(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join("docs", "x.txt"), []byte("hello\n"))
	a := learnSeq(t, r, "a", "grep", "read")
	c := learnSeq(t, r, "c", "read", "grep")
	b := learnSeq(t, r, "b", "grep", "read", "grep")
	for i := range 3 {
		args := map[string]any{"step1": map[string]string{"path": "notes.txt"}, "step2": map[string]string{"path": "docs", "pattern": "hello"}}
		if out, _, failed := r.call(t, fmt.Sprintf("c%d", i), "run_routine", map[string]any{"name": c, "args": args}, "allow"); failed {
			t.Fatal(out)
		}
	}
	idx := routineIndex(t, r, "anything")
	if !strings.Contains(idx, "- "+a+":") || !strings.Contains(idx, "- "+c+":") || strings.Contains(idx, "- "+b+":") {
		t.Fatalf("want a and c listed, b hidden:\n%s", idx)
	}
}

// A dropped routine returns to the index only after a run that succeeds: a call refused before it runs
// (here: an unknown input) leaves it out.
func TestADroppedRoutineReturnsOnlyAfterASuccessfulRun(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join(".magi", "routines", "unused", "routine.md"), routineDoc("never picked"))
	for i := range 20 {
		routineIndex(t, r, fmt.Sprintf("turn %d", i))
	}
	r.fire("idle")
	if idx := routineIndex(t, r, "after idle"); strings.Contains(idx, "- unused:") {
		t.Fatalf("not dropped:\n%s", idx)
	}
	if _, _, failed := r.call(t, "bad", "run_routine", map[string]any{"name": "unused", "args": map[string]any{"bogus": "x"}}, "allow"); !failed {
		t.Fatal("the call was meant to be refused")
	}
	if idx := routineIndex(t, r, "after a refused call"); strings.Contains(idx, "- unused:") {
		t.Fatalf("a call that never ran brought the routine back:\n%s", idx)
	}
	if out, _, failed := r.call(t, "good", "run_routine", map[string]any{"name": "unused"}, "allow"); failed {
		t.Fatal(out)
	}
	if idx := routineIndex(t, r, "after a run"); !strings.Contains(idx, "- unused:") {
		t.Fatalf("a successful run did not bring the routine back:\n%s", idx)
	}
}

// routineSkill finds the skill the model is offered for a routine ("" if none).
func routineSkill(r *routineRig, name string) string {
	for _, sk := range r.a.availableSkills(r.s.Workdir) {
		if sk.Name == name {
			return sk.Description
		}
	}
	return ""
}

// Routines reach the model the way skills do: each chosen routine becomes a <name>.routine skill whose
// one line says to call run_routine directly. A routine that already is a flat skill file is not listed
// twice, and the skill files never show up as routines of their own.
func TestSavedRoutinesReachTheModelAsSkills(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join(".magi", "routines", "later", "routine.md"), routineDoc("count lines saved mid-session"))
	r.fire("user_message", "text", "hello there")
	desc := routineSkill(r, "later.routine")
	if !strings.Contains(desc, "count lines saved mid-session") || !strings.Contains(desc, `run_routine {"name":"later"}`) {
		t.Fatalf("routine not offered as a skill: %q", desc)
	}
	if routineSkill(r, "readit.routine") != "" {
		t.Fatal("a routine that already is a skill file was listed twice")
	}
	catalog, _, failed := r.call(t, "cat", "routine_catalog", nil, "allow")
	if failed || strings.Contains(catalog, "later.routine") || !strings.Contains(catalog, "later") {
		t.Fatalf("skill files leaked into the routine catalog: %s", catalog)
	}
}

// A routine taken out of the list loses its skill file; files the plug-in did not write are left alone.
func TestARoutineLeftOutOfTheListLeavesTheSkills(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.put(t, filepath.Join(".magi", "routines", "unused", "routine.md"), routineDoc("never picked"))
	mine := filepath.Join(".magi", "skills", "mine.routine.md")
	r.put(t, mine, []byte("written by a person\nbody\n"))
	for i := range 20 {
		r.fire("user_message", "text", fmt.Sprintf("turn %d", i))
	}
	if routineSkill(r, "unused.routine") == "" {
		t.Fatal("not offered before the review")
	}
	r.fire("idle")
	r.fire("user_message", "text", "after idle")
	if routineSkill(r, "unused.routine") != "" {
		t.Fatal("a routine dropped from the list is still offered as a skill")
	}
	if _, err := os.Stat(filepath.Join(r.s.Workdir, mine)); err != nil {
		t.Fatalf("a skill file the plug-in did not write was removed: %v", err)
	}
}
