package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A design token that is not defined anywhere resolves to nothing, and the rule it was in is
// silently dropped.
//
// There is no error, no console warning, and nothing on screen to look at: the element just draws
// as if that property had never been written. Measured three times in one stylesheet — a legend
// dot asking for `--md-sys-shape-corner-full` (this tree names it `--magi-sys-shape-full`, and the
// Material bundle only READS that name, it does not define it) came out square, and the meeting
// minutes had been shipping `--magi-sys-space-050` for its line spacing where the scale is
// `--magi-sys-space-50`, so five rules had been laying out at zero gap since the day they landed.
//
// A `var(--x, fallback)` is exempt: writing a fallback is saying out loud that the token may be
// absent, which is the honest form and is how a rule borrows a token the component owns.
func TestEveryDesignTokenUsedIsDefined(t *testing.T) {
	path := filepath.Join("..", "ui", "console.css")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the stylesheet this guard exists for is not where it was: %v", err)
	}
	css := string(b)

	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--[A-Za-z0-9-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	// Only var() calls with NO fallback: the second group is the comma.
	var missing []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`var\(\s*(--[A-Za-z0-9-]+)\s*(,)?`).FindAllStringSubmatch(css, -1) {
		if m[2] != "" || defined[m[1]] || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		missing = append(missing, m[1])
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("console.css uses %d token(s) nothing defines, so every rule holding one is "+
			"silently dropped: %s\n\nEither define it beside the others on :root, or write a "+
			"fallback — var(--x, 8px) — to say the absence is expected.",
			len(missing), strings.Join(missing, ", "))
	}
	// The guard has to be reading a real stylesheet, not an empty string that trivially passes.
	if len(defined) < 50 {
		t.Errorf("only %d token definitions were found in %s — this guard is measuring nothing",
			len(defined), path)
	}
}
