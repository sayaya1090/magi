package app

import (
	"strings"
	"testing"
)

// Live, one worker's brief carried both of these, eleven lines apart:
//
//	⟨semantic⟩ Run-length compression … → caml_fl_sweep / free_list maintenance
//	USE: Fix the free-list traversal bug in caml_fl_sweep …
//	…
//	- `caml_fl_sweep` — CONFIRMED ABSENT: searched again across the whole workspace…
//
// Two authority blocks telling it opposite things, and the one that reads like a directive is the
// wrong one. Appending the settlement is not enough — it has to reach the line.
func TestAbsentNameIsCorrectedWhereItIsUsed(t *testing.T) {
	mined := "- ⟨hard⟩ make -C testsuite one DIR=tests/basic → Exit code must be 0 → testsuite execution\n" +
		"- ⟨semantic⟩ Run-length compression of free list → well-formed free list → caml_fl_sweep / free_list maintenance\n" +
		"USE: Fix the free-list traversal bug in caml_fl_sweep to restore GC correctness."
	out := annotateAbsent(mined, []string{"caml_fl_sweep"})

	lines := strings.Split(out, "\n")
	if strings.Contains(lines[0], "DOES NOT EXIST") {
		t.Error("a line that never used the name must be untouched")
	}
	for _, i := range []int{1, 2} {
		if !strings.Contains(lines[i], "DOES NOT EXIST") {
			t.Errorf("line %d uses the absent name and must say so: %q", i, lines[i])
		}
	}
	if !strings.Contains(out, "This line's INTENT stands; the name does not") {
		t.Error("the request's intent must survive the correction — only the name is wrong")
	}
	if !strings.Contains(out, "make -C testsuite one DIR=tests/basic") {
		t.Error("nothing may be dropped from the contract")
	}
	// The settlement's own lines must not be annotated with themselves.
	settled := "- `caml_fl_sweep` — CONFIRMED ABSENT: searched again across the whole workspace."
	if annotateAbsent(settled, []string{"caml_fl_sweep"}) != settled {
		t.Error("the settlement block must be left alone")
	}
	// No confirmed absence changes nothing.
	if annotateAbsent(mined, nil) != mined {
		t.Error("with nothing settled the contract is untouched")
	}
}

// correctMinedAbsences must be a no-op when there is no stored contract to reach into.
func TestCorrectMinedAbsencesNeedsAContract(t *testing.T) {
	a := newShellApp(t, &shellPlatform{})
	const sid = "s_main"
	a.correctMinedAbsences(sid, []string{"caml_fl_sweep"})
	if got := a.cachedSpecMine(sid); got != "" {
		t.Errorf("nothing stored must stay nothing, got %q", got)
	}
	a.storeSpecMine(sid, "USE: fix caml_fl_sweep")
	a.correctMinedAbsences(sid, []string{"caml_fl_sweep"})
	if !strings.Contains(a.cachedSpecMine(sid), "DOES NOT EXIST") {
		t.Errorf("the stored contract must be rewritten, got %q", a.cachedSpecMine(sid))
	}
}
