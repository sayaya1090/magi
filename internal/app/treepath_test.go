package app

import (
	"path/filepath"
	"testing"
)

// The perimeter has to answer in the OS's own separator, because that is what it is asked in.
//
// Every caller feeds this a `filepath.Rel` result, and Rel answers with the platform separator. The
// six copies of this rule tested for `"../"`, so on Windows an escaping path — `..\thing` — read as
// one that stays inside. That is the permissive direction in all six: a path outside the workspace
// read as in-tree, somebody else's directory read as the run's own.
//
// Written with filepath.Join rather than literals so the test says the same thing on both
// platforms: on POSIX these are `../x`, on Windows `..\x`, and both must escape.
func TestEscapingIsJudgedInThePlatformsOwnSeparator(t *testing.T) {
	for name, rel := range map[string]string{
		"one hop up":        "..",
		"up and across":     filepath.Join("..", "elsewhere"),
		"twice up":          filepath.Join("..", "..", "etc"),
		"up then back down": filepath.Join("..", "sibling", "file.txt"),
	} {
		if !escapesTree(rel) {
			t.Errorf("%s (%q): 트리를 벗어나는데 안 벗어난다고 답했다 — 경계가 열리는 쪽으로 틀린다", name, rel)
		}
	}
	for name, rel := range map[string]string{
		"the base itself":      ".",
		"a file in it":         "main.c",
		"a file below it":      filepath.Join("src", "main.c"),
		"a name that looks up": "..hidden",
		// `..stuff` and `../stuff` differ by one character and by everything else.
		"a directory that looks up": filepath.Join("..hidden", "x"),
	} {
		if escapesTree(rel) {
			t.Errorf("%s (%q): 트리 안인데 벗어난다고 답했다", name, rel)
		}
	}
}

// A tree holds what is at or under it, and `src` does not hold `srcs`.
//
// Both halves are one mistake: the prefix has to carry a separator or a sibling whose name merely
// starts the same is swept in, and it has to be the OS's separator or nothing matches at all — which
// is what made `rm -rf src` on a directory holding the person's file pass unasked on Windows.
func TestATreeHoldsWhatIsAtOrUnderIt(t *testing.T) {
	for name, c := range map[string]struct{ path, rel string }{
		"the directory itself": {"src", "src"},
		"a file in it":         {filepath.Join("src", "theirs.c"), "src"},
		"deeper still":         {filepath.Join("src", "a", "b", "c.h"), "src"},
	} {
		if !underTree(c.path, c.rel) {
			t.Errorf("%s: %q 가 %q 아래인데 아니라고 답했다", name, c.path, c.rel)
		}
	}
	for name, c := range map[string]struct{ path, rel string }{
		"a sibling that starts the same": {"srcs", "src"},
		"a file that starts the same":    {"src.bak", "src"},
		"a sibling tree":                 {filepath.Join("srcs", "x.c"), "src"},
		"something above it":             {"main.c", "src"},
		"the same name one level deeper": {filepath.Join("vendor", "src"), "src"},
		"a prefix without the separator": {"srcx", "src"},
		"the other way round":            {"src", filepath.Join("src", "a")},
	} {
		if underTree(c.path, c.rel) {
			t.Errorf("%s: %q 는 %q 아래가 아닌데 그렇다고 답했다 — 남의 것을 쓸어 간다", name, c.path, c.rel)
		}
	}
}
