package idebridge

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// declaredWords returns every exported string constant declared in one file of this package.
//
// ⚠ **Because a list written by hand cannot notice a word it was never told about.** Both
// vocabulary guards used to hold the TypeScript copy against a literal restated in the test — and
// this package's own doctrine names that shape: "a list that only checks itself would pass with
// the entry deleted" (tool/builtin/withheld_test.go). Measured 2026-09-11: adding
// `Paused = "paused"` to activity.go left TestBothCopiesSpeakOneVocabulary green, still logging
// that it had compared five words. A word this side speaks and the editors do not is exactly the
// drift these guards exist for, and it was the one thing they could not see.
//
// Parsed rather than grepped: the files carry long doc comments that quote these very words, and a
// text scan reads the prose as declarations.
func declaredWords(t *testing.T, file string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("%s 를 못 읽었다 — 스캔이 깨진 것이지 어휘가 맞는 게 아니다: %v", file, err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			v, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range v.Names {
				if !name.IsExported() || i >= len(v.Values) {
					continue
				}
				lit, ok := v.Values[i].(*ast.BasicLit)
				if ok && lit.Kind == token.STRING && len(lit.Value) >= 2 {
					out[name.Name] = lit.Value[1 : len(lit.Value)-1]
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s 에서 상수를 하나도 못 찾았다 — 스캔이 깨졌다", file)
	}
	return out
}
