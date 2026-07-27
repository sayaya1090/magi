package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func greppedEvents(id, pattern, path, glob string, hits []string, isErr bool) []event.Event {
	args, _ := json.Marshal(map[string]string{"pattern": pattern, "path": path, "glob": glob})
	call, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
			CallID: id, Name: "grep", Args: args,
		}},
	})
	content, _ := json.Marshal(hits)
	res, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleTool,
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: id, Content: content, IsError: isErr,
		}},
	})
	return []event.Event{
		{Type: event.TypePartAppended, Data: call},
		{Type: event.TypePartAppended, Data: res},
	}
}

// The fact being salvaged spans a call and ITS OWN result — the pattern is in the arguments, the
// outcome in the result — so the pairing is by call id, and a search that matched must never be
// reported as an absence.
func TestSearchMissesInPairsCallsWithTheirOwnResults(t *testing.T) {
	var evs []event.Event
	evs = append(evs, greppedEvents("c1", "caml_fl_sweep", "", "", nil, false)...)
	evs = append(evs, greppedEvents("c2", "free_list", "", "", []string{"runtime/afl.c:12:free_list"}, false)...)
	evs = append(evs, greppedEvents("c3", "sweep_slice", "runtime", "*.c", nil, false)...)

	got := searchMissesIn(evs)
	if len(got) != 2 {
		t.Fatalf("misses = %+v, want the two searches that found nothing", got)
	}
	if got[0].pattern != "caml_fl_sweep" || got[0].scope != "anywhere in the workspace" {
		t.Errorf("first miss = %+v, want the unscoped search reported as workspace-wide", got[0])
	}
	if got[1].pattern != "sweep_slice" || !strings.Contains(got[1].scope, "`runtime`") ||
		!strings.Contains(got[1].scope, "`*.c`") {
		t.Errorf("second miss = %+v, want the scope carried — a miss under one directory says less "+
			"than a miss across the tree", got[1])
	}
}

// A pattern that missed under a narrow scope and later matched across the tree is NOT absent. The
// hit can arrive after the miss, so the filter has to run at the end rather than inline.
func TestSearchMissesInDropsAPatternThatMatchedLater(t *testing.T) {
	var evs []event.Event
	evs = append(evs, greppedEvents("c1", "caml_fl_sweep", "runtime/caml", "", nil, false)...)
	evs = append(evs, greppedEvents("c2", "caml_fl_sweep", "", "", []string{"runtime/afl.c:9:caml_fl_sweep"}, false)...)

	if got := searchMissesIn(evs); len(got) != 0 {
		t.Fatalf("misses = %+v, want none — the identifier IS in the tree", got)
	}
}

// A search magi could not run, or a result it cannot read, proves nothing about the repository. A
// missing record must never be promoted to a proven absence, so every unreadable shape stays silent.
func TestSearchMissesInStaysSilentWhenItCannotJudge(t *testing.T) {
	errored := greppedEvents("c1", "caml_fl_sweep", "", "", nil, true) // invalid regex / refused call
	if got := searchMissesIn(errored); len(got) != 0 {
		t.Errorf("an errored search yielded %+v, want nothing", got)
	}
	// A result shape grep does not produce (a bare string, not an array of lines).
	odd := greppedEvents("c2", "x_symbol", "", "", nil, false)
	bad, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleTool,
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: "c2", Content: json.RawMessage(`"no matches"`),
		}},
	})
	odd[1] = event.Event{Type: event.TypePartAppended, Data: bad}
	if got := searchMissesIn(odd); len(got) != 0 {
		t.Errorf("an unreadable result yielded %+v, want nothing", got)
	}
	// A call with no result at all (the guard stopped the child mid-flight).
	if got := searchMissesIn(odd[:1]); len(got) != 0 {
		t.Errorf("an unanswered call yielded %+v, want nothing", got)
	}
	// Only grep is evidence: a glob's empty answer is about filenames and is far easier to write in a
	// shape that can never match, which reads exactly like an empty directory.
	if got := searchMissesIn([]event.Event{toolCallEvent("glob", `{"pattern":"*.h","path":"runtime/caml"}`)}); len(got) != 0 {
		t.Errorf("a glob yielded %+v, want nothing", got)
	}
	if got := searchMissesIn(nil); len(got) != 0 {
		t.Errorf("no events yielded %+v, want nothing", got)
	}
}

// One line per pattern however many times it was searched, and a bounded list — an explorer that
// spun through dozens of searches has little to say, and a long list crowds out the planner's own
// grounding.
func TestSearchMissesInDedupesAndCaps(t *testing.T) {
	var evs []event.Event
	evs = append(evs, greppedEvents("a1", "same_symbol", "", "", nil, false)...)
	evs = append(evs, greppedEvents("a2", "same_symbol", "runtime", "", nil, false)...)
	if got := searchMissesIn(evs); len(got) != 1 {
		t.Fatalf("misses = %+v, want one line for the repeated pattern", got)
	}
	evs = nil
	for i := 0; i < exploreNegCap+5; i++ {
		evs = append(evs, greppedEvents(string(rune('a'+i)), "sym_"+string(rune('a'+i)), "", "", nil, false)...)
	}
	if got := searchMissesIn(evs); len(got) != exploreNegCap {
		t.Fatalf("misses = %d, want the list capped at %d", len(got), exploreNegCap)
	}
}

// The note must say where it came from and what it licenses. A reader that mistook it for
// repository findings would have it exactly backwards — these names are the ones NOT to build on.
func TestRenderSearchMissesStatesTheOneInferenceItLicenses(t *testing.T) {
	out := renderSearchMisses([]searchMiss{{pattern: "caml_fl_sweep", scope: "anywhere in the workspace"}})
	for _, want := range []string{"Searched and NOT found", "matched NOTHING", "`caml_fl_sweep`",
		"magi's own record", "must CREATE it"} {
		if !strings.Contains(out, want) {
			t.Errorf("note = %q\n  missing %q", out, want)
		}
	}
	if renderSearchMisses(nil) != "" {
		t.Error("no misses must render no note")
	}
}

// The salvage reads the child's log out of the store, and an unreadable or unnamed session must
// yield nothing rather than an error — the discard path it runs on is already the failure path.
func TestSearchedNotFoundReadsTheChildSession(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid := startSession(t, a, t.TempDir())
	for _, e := range greppedEvents("c1", "caml_fl_sweep", "", "", nil, false) {
		e.SessionID = sid
		if _, err := a.store.Append(ctx, sid, e); err != nil {
			t.Fatal(err)
		}
	}
	got := a.searchedNotFound(ctx, sid)
	if len(got) != 1 || got[0].pattern != "caml_fl_sweep" {
		t.Fatalf("searchedNotFound = %+v, want the one recorded absence", got)
	}
	if n := a.searchedNotFound(ctx, ""); len(n) != 0 {
		t.Errorf("an unnamed session yielded %+v, want nothing", n)
	}
	if n := a.searchedNotFound(ctx, session.SessionID("s-does-not-exist")); len(n) != 0 {
		t.Errorf("an unreadable session yielded %+v, want nothing", n)
	}
}

// The salvaged note has to reach the same two places the ordinary findings note does — the mined
// contract the termination council reads, and the session the planner and check-author read — or it
// is salvage that changes nothing.
func TestInjectSpecMineNoteFoldsIntoTheMinedContract(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid := startSession(t, a, t.TempDir())

	a.injectSpecMineNote(ctx, sid, "first note")
	a.injectSpecMineNote(ctx, sid, "second note")
	got := a.cachedSpecMine(sid)
	if !strings.Contains(got, "first note") || !strings.Contains(got, "second note") {
		t.Fatalf("mined contract = %q, want both notes — a second injection must append, not replace", got)
	}
	a.injectSpecMineNote(ctx, sid, "   ")
	if a.cachedSpecMine(sid) != got {
		t.Error("an empty note must change nothing")
	}
}

func TestSearchScopeNamesWhatWasActuallyCovered(t *testing.T) {
	cases := []struct{ path, glob, want string }{
		{"", "", "anywhere in the workspace"},
		{"runtime", "", "anywhere under `runtime`"},
		{"", "*.c", "in any `*.c` file in the workspace"},
		{"runtime", "*.c", "in any `*.c` file under `runtime`"},
	}
	for _, c := range cases {
		if got := searchScope(c.path, c.glob); got != c.want {
			t.Errorf("searchScope(%q,%q) = %q, want %q", c.path, c.glob, got, c.want)
		}
	}
}
