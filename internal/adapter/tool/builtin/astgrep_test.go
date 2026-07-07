package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// parseAstGrepStream turns ast-grep's --json=stream output into workdir-relative,
// 1-based matches, tolerating malformed lines.
func TestParseAstGrepStream(t *testing.T) {
	// Two valid stream objects + one malformed line that must be skipped.
	out := []byte(`{"file":"/work/auth.go","lines":"return db.QueryRow(q)","range":{"start":{"line":9,"column":1}}}
{"file":"/work/sub/calc.go","text":"sum / n","range":{"start":{"line":4,"column":8}}}
{not json}
`)
	got := parseAstGrepStream(out, "/work")
	if len(got) != 2 {
		t.Fatalf("want 2 matches, got %d: %+v", len(got), got)
	}
	if got[0].File != "auth.go" || got[0].Line != 10 { // 0-based 9 -> 1-based 10
		t.Errorf("match0 = %+v, want auth.go:10", got[0])
	}
	if got[1].File != "sub/calc.go" || got[1].Line != 5 {
		t.Errorf("match1 = %+v, want sub/calc.go:5", got[1])
	}
	if got[0].Text != "return db.QueryRow(q)" {
		t.Errorf("match0 text = %q", got[0].Text)
	}
}

// When ast-grep isn't installed the tool must fail gracefully and point the model
// at the fallback search tools rather than erroring opaquely.
func TestAstGrepFallbackWhenMissing(t *testing.T) {
	if _, ok := astGrepBin(); ok {
		t.Skip("ast-grep is installed; fallback path not exercised")
	}
	raw, _ := json.Marshal(astGrepArgs{Pattern: "$X == nil"})
	res, err := AstGrep{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected an error result when ast-grep is missing")
	}
	if s := string(res.Content); !strings.Contains(s, "grep") || !strings.Contains(s, "findcontext") {
		t.Errorf("missing-binary message should suggest grep/findcontext, got: %s", s)
	}
}

// End-to-end structural search via the real ast-grep CLI. Skips when ast-grep isn't
// on PATH (e.g. CI/local without it), so it exercises the Execute → CLI → stream
// parse path when the binary is present without flaking the hermetic build.
func TestAstGrepRealMatch(t *testing.T) {
	if _, ok := astGrepBin(); !ok {
		t.Skip("ast-grep not installed")
	}
	dir := t.TempDir()
	src := "package x\n\nfunc F(p *int) bool {\n\tif p == nil {\n\t\treturn true\n\t}\n\treturn false\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Decoy file with no nil comparison — must NOT match.
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package x\n\nfunc G() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(astGrepArgs{Pattern: "$A == nil", Lang: "go"})
	res, err := AstGrep{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("structural search errored: %s", res.Content)
	}
	// The result is a JSON array of matches; decode and assert the nil comparison
	// in a.go was found (and only there).
	var matches []astMatch
	if err := json.Unmarshal(res.Content, &matches); err != nil {
		t.Fatalf("decode matches: %v (content=%s)", err, res.Content)
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly 1 match, got %d: %+v", len(matches), matches)
	}
	if matches[0].File != "a.go" || matches[0].Line != 4 {
		t.Errorf("match = %+v, want a.go:4", matches[0])
	}
}

// A valid pattern that simply matches nothing must report "no structural matches",
// not a pattern/lang error: ast-grep exits 1 on no-match (like grep), and treating
// that as failure made the model distrust a correct query.
func TestAstGrepNoMatchIsNotError(t *testing.T) {
	if _, ok := astGrepBin(); !ok {
		t.Skip("ast-grep not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Well-formed Go pattern with no possible match in this file.
	raw, _ := json.Marshal(astGrepArgs{Pattern: "$A + $B + $C + $D", Lang: "go"})
	res, err := AstGrep{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("a valid no-match search must not be an error; got %q", res.Content)
	}
	if !strings.Contains(string(res.Content), "no structural matches") {
		t.Errorf("want \"no structural matches\"; got %q", res.Content)
	}
}

// A genuinely bad lang (ast-grep exit 2) must still surface as an error, so the
// exit-1 carve-out doesn't swallow real failures.
func TestAstGrepBadLangStillErrors(t *testing.T) {
	if _, ok := astGrepBin(); !ok {
		t.Skip("ast-grep not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(astGrepArgs{Pattern: "$A == nil", Lang: "klingon"})
	res, err := AstGrep{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("a bad lang (exit 2) must remain an error; got %q", res.Content)
	}
}
