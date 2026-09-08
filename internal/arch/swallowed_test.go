package arch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A discarded return is how a failure stops being one. `_ = f()` and `_, _ = f()` compile, read as
// deliberate, and are the shape every silent defect found in this codebase arrived in — a glob that
// answered `[]` instead of an error, an argument dropped before a call, a job reporting someone
// else's exit.
//
// Most of the 149 already here are fine: a Close in a defer, a write to a debug log, a marshal of a
// struct that cannot fail. Auditing them all is not this test's job and forbidding the pattern
// outright would fail on the first run and be deleted by the second. What it does instead is hold
// the number: these files may keep what they have, and the next one has to be argued for.
//
// Rebaseline deliberately, after removing some or accepting a new one:
//
//	MAGI_RATCHET_UPDATE=1 go test ./internal/arch/
//
// and say in the commit which way it moved and why.
const baselineFile = "swallowed_baseline.json"

// blankAssign matches an assignment whose left side is nothing but blanks: `_ =`, `_, _ =`, and so
// on. One pattern rather than several, so a line is counted once. `==` is excluded — a comparison
// discards nothing.
var blankAssign = regexp.MustCompile(`^\s*_(?:\s*,\s*_)*\s*=[^=]`)

type baseline struct {
	Total int            `json:"total"`
	Files map[string]int `json:"files"`
}

// scannerSamples pins what blankAssign must and must not match, as literal lines.
//
// Without this the ratchet cannot tell a cleaner tree from a scanner that stopped scanning. Both
// arrive as a smaller number, and a smaller number is the case it does not fail on — it logs
// "down to N from 141 — rebaseline to keep the ratchet tight", which is an invitation to write
// the blindness into the baseline. Measured: with the pattern replaced by one that matches
// nothing, the test passed, reported 0 discarded returns in 0 files, and printed that line.
//
// A guard whose whole subject is failures that pass silently was passing its own silently.
var scannerSamples = []struct {
	line string
	want bool
}{
	{`_ = f()`, true},
	{"\t_ = json.Unmarshal(b, &v)", true},
	{`_, _ = io.Copy(w, r)`, true},
	{"\t\t_, _, _ = three()", true},
	{`  _  ,  _  = spaced()`, true},
	// Not a discard: a comparison keeps everything, and an assignment that keeps a value is a
	// different shape this does not hold (see the note on scope in the test below).
	{`if a == b {`, false},
	{`x, _ := f()`, false},
	{`_ == other`, false},
	{`return _foo = 1`, false},
}

// checkScanner fails when the pattern no longer does what the counts are read as meaning.
func checkScanner(t *testing.T) {
	t.Helper()
	for _, s := range scannerSamples {
		if got := blankAssign.MatchString(s.line); got != s.want {
			t.Fatalf("the scanner is broken, so the count below would mean nothing: "+
				"blankAssign.MatchString(%q) = %v, want %v", s.line, got, s.want)
		}
	}
}

func countSwallowed(t *testing.T) (map[string]int, int) {
	t.Helper()
	checkScanner(t)
	counts := map[string]int{}
	total := 0
	for _, f := range goFiles(t) {
		b, err := os.ReadFile(filepath.Join("..", "..", f))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, line := range strings.Split(string(b), "\n") {
			if blankAssign.MatchString(line) {
				n++
			}
		}
		if n > 0 {
			counts[f] = n
			total += n
		}
	}
	return counts, total
}

func TestSwallowedErrorsDoNotGrow(t *testing.T) {
	counts, total := countSwallowed(t)

	if os.Getenv("MAGI_RATCHET_UPDATE") != "" {
		b, err := json.MarshalIndent(baseline{Total: total, Files: counts}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(baselineFile, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("baseline rewritten: %d files, %d occurrences", len(counts), total)
		return
	}

	raw, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Fatalf("%v — create it with MAGI_RATCHET_UPDATE=1 go test ./internal/arch/", err)
	}
	var base baseline
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}
	if base.Total == 0 || len(base.Files) == 0 {
		t.Fatal("the baseline is empty, so this test would pass over anything")
	}

	// Per file, because a total alone lets one file absorb another's cleanup.
	var grew, added []string
	for f, n := range counts {
		was, known := base.Files[f]
		switch {
		case !known:
			added = append(added, f)
		case n > was:
			grew = append(grew, f)
			t.Errorf("%s: %d discarded returns, was %d — propagate it, log it, or say why it is "+
				"best-effort. If it is right anyway: MAGI_RATCHET_UPDATE=1 go test ./internal/arch/",
				f, n, was)
		}
	}
	sort.Strings(added)
	for _, f := range added {
		t.Errorf("%s is a new file and starts with %d discarded returns — a new file has no "+
			"history to inherit, so handle them or rebaseline deliberately", f, counts[f])
	}
	if total > base.Total {
		t.Errorf("%d discarded returns across the tree, was %d", total, base.Total)
	}
	// Progress is recorded, not silently absorbed: a tree that got cleaner should say so, and the
	// baseline should follow it down.
	if total < base.Total {
		t.Logf("down to %d from %d — rebaseline to keep the ratchet tight "+
			"(MAGI_RATCHET_UPDATE=1 go test ./internal/arch/)", total, base.Total)
	}
	t.Logf("%d discarded returns in %d files (baseline %d)", total, len(counts), base.Total)
}
