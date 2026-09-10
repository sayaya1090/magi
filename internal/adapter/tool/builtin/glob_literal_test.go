package builtin

import (
	"path"
	"path/filepath"
	"testing"
)

// A file whose name contains a glob character is still a file, and it must be findable.
//
// The way to ask for one is to escape the character — `page\[1\]` — and that is what the console's
// name box builds. It worked nowhere on Windows: `filepath.Match` DISABLES escaping there and reads
// `\` as a path separator instead, so the pattern became segments that match nothing. The console
// answered "no such file" about a file sitting in the workspace, with no error to look at.
//
// There is no other spelling to fall back on. Go's Match has no character-class form for a literal
// bracket — `[[]` and `[]]` are both syntax errors — so on Windows `filepath.Match` cannot match one
// at all. The matcher had to change, not the quoting.
//
// Both sides are split on "/" before they get here, so a segment never holds a separator and
// path.Match is simply the right function: same reading on every platform, escapes included.
func TestALiteralGlobCharacterIsMatchableByEscapingIt(t *testing.T) {
	for name, c := range map[string]struct{ pattern, file string }{
		"a bracket":          {`page\[1\].txt`, "page[1].txt"},
		"a bracket, in **":   {`**/page\[1\].txt`, "docs/page[1].txt"},
		"a star":             {`a\*b.txt`, "a*b.txt"},
		"a question mark":    {`q\?.txt`, "q?.txt"},
		"contains, as typed": {`*page\[1\]*`, "page[1].txt"},
	} {
		if !matchGlob(c.pattern, c.file) {
			t.Errorf("%s: %q 가 %q 를 못 잡는다 — 이 이름의 파일은 검색으로 못 찾는다", name, c.pattern, c.file)
		}
	}

	// The escape must not become a wildcard by accident: an escaped bracket asks for THAT name.
	for name, c := range map[string]struct{ pattern, file string }{
		"the class it would have been": {`page\[1\].txt`, "page1.txt"},
		"a neighbour":                  {`page\[1\].txt`, "page[2].txt"},
		"a star that is not one":       {`a\*b.txt`, "axxb.txt"},
	} {
		if matchGlob(c.pattern, c.file) {
			t.Errorf("%s: %q 가 %q 까지 잡는다 — 인용이 와일드카드가 됐다", name, c.pattern, c.file)
		}
	}

	// And an unescaped class still IS a class, or every existing pattern changed meaning.
	if !matchGlob("page[12].txt", "page1.txt") {
		t.Error("문자 클래스가 더 이상 클래스가 아니다")
	}
	if !matchGlob("**/*.go", "internal/app/x.go") {
		t.Error("평범한 패턴이 깨졌다")
	}
}

// The up-front validation and the matcher must be the same function.
//
// A pattern accepted by one and refused by the other is the worst of both: the call is let in and
// then quietly matches nothing. They were two functions — `filepath.Match` to validate, and the
// same to match — and moving only one would have created exactly that split, so this pins them
// together rather than pinning either one's identity.
func TestTheValidatorAgreesWithTheMatcher(t *testing.T) {
	for _, seg := range []string{`page\[1\].txt`, "page[12].txt", "*.go", "a\\*b", "[", "[a-]"} {
		_, verr := path.Match(seg, "")
		_, merr := path.Match(seg, "anything")
		if (verr == nil) != (merr == nil) {
			t.Errorf("%q: 검사와 매칭의 판정이 갈린다 (%v / %v)", seg, verr, merr)
		}
	}
	// And the function this used to be is named, so the difference is on the record rather than
	// implied by its absence. On Unix the two agree; on Windows filepath.Match reads the escape as
	// a separator and answers false, which is the whole defect.
	const pattern, file = `page\[1\].txt`, "page[1].txt"
	old, _ := filepath.Match(pattern, file)
	if new := mustMatch(t, pattern, file); !new {
		t.Fatalf("path.Match(%q, %q) 가 false — 이 시험의 전제가 무너졌다", pattern, file)
	} else if !old {
		t.Logf("이 플랫폼에서 filepath.Match 는 %q 로 %q 를 못 잡는다 — path.Match 를 쓰는 이유다", pattern, file)
	}
}

func mustMatch(t *testing.T, pattern, name string) bool {
	t.Helper()
	ok, err := path.Match(pattern, name)
	if err != nil {
		t.Fatalf("path.Match(%q, %q): %v", pattern, name, err)
	}
	return ok
}
