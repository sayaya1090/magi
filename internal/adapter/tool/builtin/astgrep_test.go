package builtin

import (
	"context"
	"encoding/json"
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
