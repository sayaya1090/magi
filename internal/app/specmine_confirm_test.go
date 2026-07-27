package app

import (
	"strings"
	"testing"
)

// The detector's whole job is to notice that a claim uses a name magi's own record says is absent.
// These lock the two halves that decide whether it fires at all: what counts as a name, and where in
// a plan a name is allowed to hide.
func TestContradictedNamesFindsTheNameWhereverThePlanPutIt(t *testing.T) {
	miss := []searchMiss{{pattern: "widget_reap_all", scope: "anywhere under `src`"}}
	for _, tc := range []struct {
		name string
		step planStep
		want bool
	}{
		{"title", planStep{Title: "Read widget_reap_all and fix it", Strategy: "solo"}, true},
		// renderSteps prints neither of these, which is why the detector reads the fields directly:
		// a delegate's task is the longest free text in a plan and the likeliest place for a name
		// the planner invented.
		{"delegate task", planStep{Strategy: "delegate", Task: "Rewrite widget_reap_all to use the new list"}, true},
		{"scout each", planStep{Strategy: "scout", Discover: "the runtime sources", Each: "does it call widget_reap_all?"}, true},
		{"parallel question", planStep{Strategy: "parallel", Groups: []planGroup{{
			Agent: "explore", Focus: "gc", Question: "where is widget_reap_all defined?"}}}, true},
		{"absent from the plan", planStep{Title: "Read the build docs", Strategy: "solo"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := contradictedNames(planText([]planStep{tc.step}), miss)
			if (len(got) > 0) != tc.want {
				t.Fatalf("contradictedNames = %v, want fired=%v", got, tc.want)
			}
		})
	}
}

// A miss recorded for an alternation is a miss for every branch: the pattern matched NOTHING, so no
// branch matched either. That inference is what lets a `grep -E "a|b|c"` contribute negatives at all
// — without it the whole search is silently worth nothing.
func TestContradictedNamesSplitsAnAlternationButRefusesARegex(t *testing.T) {
	plan := planText([]planStep{{Title: "Fix caml_sweep_all in the collector", Strategy: "solo"}})
	got := contradictedNames(plan, []searchMiss{{pattern: "caml_sweep_all|caml_mark_all", scope: "in the tree"}})
	if len(got) != 1 || got[0].pattern != "caml_sweep_all" {
		t.Fatalf("alternation split = %v, want just the branch the plan uses", got)
	}
	// Structure means the pattern is not a name, and reading it as a literal would let `.` or a
	// character class stand for text nobody searched for.
	for _, pat := range []string{"caml_.*_all", "caml_sweep_all|[a-z]+", "^caml", "sweep(_all)?"} {
		if got := contradictedNames("caml_sweep_all caml_ x_all", []searchMiss{{pattern: pat}}); len(got) != 0 {
			t.Fatalf("pattern %q fired %v, want no opinion on a regex", pat, got)
		}
	}
}

// The floor keeps ordinary words out. A three-letter pattern turning up somewhere in a plan's prose
// is not evidence the plan depends on a symbol by that name, and confirming it would spend a spawn
// to prove nothing.
func TestPlainNameRejectsWhatIsNotAName(t *testing.T) {
	for _, s := range []string{"gc", "fl", "", "___", "1234", "a|b"} {
		if plainName(s) {
			t.Errorf("plainName(%q) = true, want false", s)
		}
	}
	for _, s := range []string{"caml_sweep", "major_gc.c", "runtime/mem.h", "free-list"} {
		if !plainName(s) {
			t.Errorf("plainName(%q) = false, want true", s)
		}
	}
}

// A plan resting on a dozen invented names is not settled one grep at a time.
func TestContradictedNamesStopsAtTheCap(t *testing.T) {
	var ms []searchMiss
	var b strings.Builder
	for i := 0; i < confirmCap+4; i++ {
		n := "invented_symbol_" + string(rune('a'+i))
		ms = append(ms, searchMiss{pattern: n})
		b.WriteString(n + " ")
	}
	if got := contradictedNames(b.String(), ms); len(got) != confirmCap {
		t.Fatalf("doubted = %d, want capped at %d", len(got), confirmCap)
	}
}

// The absence note and the correction travel in the same injection, so a name whose absence was
// disproved has to leave the list — otherwise one note says a name is missing and, lines later, that
// it is there.
func TestDropRetractedRemovesOnlyTheDisprovedName(t *testing.T) {
	ms := []searchMiss{{pattern: "a_name"}, {pattern: "b_name"}, {pattern: "c_name"}}
	got := dropRetracted(ms, map[string]bool{"b_name": true})
	if len(got) != 2 || got[0].pattern != "a_name" || got[1].pattern != "c_name" {
		t.Fatalf("dropRetracted = %v, want a_name and c_name", got)
	}
	if got := dropRetracted(ms, nil); len(got) != 3 {
		t.Fatalf("dropRetracted with nothing retracted = %v, want the list unchanged", got)
	}
}

// The suggested name is the one part of the reply that is model prose, so parsing it must never turn
// a non-answer into a recommendation.
func TestSuggestedForReadsAnAnswerAndNotANonAnswer(t *testing.T) {
	reply := "`widget_reap_all` — ABSENT; closest existing: `widget_sweep` (src/heap.c)\n" +
		"`gadget_init` — ABSENT; closest existing: none found\n"
	if got := suggestedFor(reply, "widget_reap_all"); !strings.Contains(got, "widget_sweep") {
		t.Fatalf("suggestedFor = %q, want the proposed name", got)
	}
	if got := suggestedFor(reply, "gadget_init"); got != "" {
		t.Fatalf("suggestedFor on a none-found line = %q, want empty", got)
	}
	if got := suggestedFor("I looked around a bit and things seem fine", "widget_reap_all"); got != "" {
		t.Fatalf("suggestedFor on prose with no verdict = %q, want empty", got)
	}
}

// searchMissesIn is now a filter over searchOutcomesIn; the hit half is what makes a retraction
// possible, and it has to survive the split.
func TestSearchOutcomesReportsHitsAlongsideMisses(t *testing.T) {
	evs := append(greppedEvents("c1", "present_name", "", "", []string{"src/a.c:1:present_name"}, false),
		greppedEvents("c2", "absent_name", "", "", nil, false)...)
	misses, hit := searchOutcomesIn(evs)
	if !hit["present_name"] {
		t.Fatalf("hit = %v, want present_name recorded as found", hit)
	}
	var names []string
	for _, m := range misses {
		names = append(names, m.pattern)
	}
	if len(misses) != 1 || misses[0].pattern != "absent_name" {
		t.Fatalf("misses = %v, want only absent_name", names)
	}
	if got := searchMissesIn(evs); len(got) != 1 || got[0].pattern != "absent_name" {
		t.Fatalf("searchMissesIn = %v, want the filter to behave as before", got)
	}
}
