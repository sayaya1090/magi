package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// runJSON executes a tool whose success payload is a JSON object (okJSON), returning
// the raw Content bytes and the error flag. The shared run() helper decodes Content
// as a string, which is only correct for okText tools.
func runObj(t *testing.T, tool port.Tool, args any, setup func(dir string)) ([]byte, bool) {
	t.Helper()
	dir := t.TempDir()
	if setup != nil {
		setup(dir)
	}
	raw, _ := json.Marshal(args)
	res, err := tool.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatalf("Execute returned error (should be in result): %v", err)
	}
	return res.Content, res.IsError
}

// asMap parses a tool's JSON object result into a generic map for assertions.
func asMap(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(content, &m); err != nil {
		t.Fatalf("result is not a JSON object: %v (%q)", err, content)
	}
	return m
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},        // trailing newline is not an extra empty line
		{"a\nb", 2},       // no trailing newline still counts the last
		{"a\r\nb\r\n", 2}, // CRLF
		{"a\rb", 2},       // bare CR
	}
	for _, c := range cases {
		if got := splitLines([]byte(c.in)); len(got) != c.want {
			t.Errorf("splitLines(%q) = %d lines, want %d (%v)", c.in, len(got), c.want, got)
		}
	}
}

func TestParseFloatCell(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want float64
	}{
		{"12", true, 12}, {" 3.5 ", true, 3.5}, {"63.2%", true, 63.2},
		{"", false, 0}, {"abc", false, 0}, {"1.2.3", false, 0},
	}
	for _, c := range cases {
		got, ok := parseFloatCell(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseFloatCell(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// coverage.out shape: "pkg/f.go:1.1,2.2 <numStmts> <count>". Coverage % =
// sum(col2 where col3>0) / sum(col2). Verifies the two-call recipe explorers use.
func TestTabulateCoverage(t *testing.T) {
	const cov = "mode: set\n" +
		"a.go:1.1,2.2 3 1\n" +
		"a.go:3.1,4.2 2 0\n" +
		"b.go:1.1,9.9 5 4\n"
	seed := func(dir string) { writeFile(dir, "coverage.out", cov) }

	// total statements = 3+2+5 = 10
	got, isErr := runObj(t, Tabulate{}, tabArgs{Path: "coverage.out", Column: 2, Op: "sum", SkipHeader: true}, seed)
	if isErr {
		t.Fatalf("total: err %s", got)
	}
	if m := asMap(t, got); m["result"].(float64) != 10 {
		t.Errorf("total statements = %v, want 10", m["result"])
	}
	// covered statements (col3 > 0) = 3+5 = 8
	got, isErr = runObj(t, Tabulate{}, tabArgs{
		Path: "coverage.out", Column: 2, Op: "sum", SkipHeader: true,
		Filter: &tabFilter{Column: 3, Op: ">", Value: 0},
	}, seed)
	if isErr {
		t.Fatalf("covered: err %s", got)
	}
	if m := asMap(t, got); m["result"].(float64) != 8 {
		t.Errorf("covered statements = %v, want 8", m["result"])
	}
}

func TestTabulateOps(t *testing.T) {
	// col1 numeric, col2 mixed (one non-numeric cell is skipped by avg/min/max)
	seed := func(dir string) { writeFile(dir, "d.csv", "10,x\n20,5\n30,7\n") }
	base := func(op string) tabArgs { return tabArgs{Path: "d.csv", Column: 1, Op: op, Delimiter: ","} }
	cases := []struct {
		op   string
		col  int
		want float64
	}{
		{"sum", 1, 60}, {"count", 1, 3}, {"avg", 1, 20}, {"min", 1, 10}, {"max", 1, 30},
		{"sum", 2, 12}, // "x" skipped, 5+7
		{"avg", 2, 6},  // (5+7)/2, non-numeric excluded from denominator
	}
	for _, c := range cases {
		a := base(c.op)
		a.Column = flexInt(c.col)
		got, isErr := runObj(t, Tabulate{}, a, seed)
		if isErr {
			t.Fatalf("%s col%d: err %s", c.op, c.col, got)
		}
		if m := asMap(t, got); m["result"].(float64) != c.want {
			t.Errorf("%s col%d = %v, want %v", c.op, c.col, m["result"], c.want)
		}
	}
}

func TestTabulateErrors(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "d.csv", "1,2\n") }
	if _, isErr := runObj(t, Tabulate{}, tabArgs{Path: "d.csv", Column: 0}, seed); !isErr {
		t.Error("column 0 should error")
	}
	if _, isErr := runObj(t, Tabulate{}, tabArgs{Path: "d.csv", Column: 1, Op: "median"}, seed); !isErr {
		t.Error("unknown op should error")
	}
	if _, isErr := runObj(t, Tabulate{}, tabArgs{Path: "nope.csv", Column: 1}, seed); !isErr {
		t.Error("missing file should error")
	}
	// out-of-range column: no numeric cells → sum 0, rows counted, not an error
	if got, isErr := runObj(t, Tabulate{}, tabArgs{Path: "d.csv", Column: 9, Delimiter: ","}, seed); isErr {
		t.Errorf("bad column should not error: %s", got)
	}
}

func TestTabulateJail(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "d.csv", "1\n") }
	if _, isErr := runObj(t, Tabulate{}, tabArgs{Path: "../../../etc/passwd", Column: 1}, seed); !isErr {
		t.Error("path escaping the jail should error")
	}
}

func TestCountMatches(t *testing.T) {
	seed := func(dir string) {
		writeFile(dir, "a.go", "func A() {}\nfunc B() {}\n")
		writeFile(dir, "sub/b.go", "func C() {}\n")
		writeFile(dir, "readme.md", "no funcs here\n")
	}
	// glob over *.go: three "func " matches across two files
	got, isErr := runObj(t, CountMatches{}, countMatchArgs{Pattern: "func ", Glob: "**/*.go"}, seed)
	if isErr {
		t.Fatalf("glob: err %s", got)
	}
	m := asMap(t, got)
	if m["matches"].(float64) != 3 || m["files_with_matches"].(float64) != 2 {
		t.Errorf("glob got matches=%v files=%v, want 3/2", m["matches"], m["files_with_matches"])
	}
	// single file, fixed string
	got, _ = runObj(t, CountMatches{}, countMatchArgs{Pattern: "func ", Path: "a.go", Fixed: true}, seed)
	if asMap(t, got)["matches"].(float64) != 2 {
		t.Errorf("single-file fixed match count wrong: %s", got)
	}
	// ignore_case
	got, _ = runObj(t, CountMatches{}, countMatchArgs{Pattern: "FUNC", Path: "a.go", IgnoreCase: true}, seed)
	if asMap(t, got)["matches"].(float64) != 2 {
		t.Errorf("ignore_case match count wrong: %s", got)
	}
	// missing pattern
	if _, isErr := runObj(t, CountMatches{}, countMatchArgs{Path: "a.go"}, seed); !isErr {
		t.Error("empty pattern should error")
	}
}

func TestCountLines(t *testing.T) {
	seed := func(dir string) {
		writeFile(dir, "a.txt", "one two\nthree\n")    // 2 lines, 3 words
		writeFile(dir, "b.txt", "x\r\ny\r\nz")         // CRLF, 3 lines, no trailing newline
		writeFile(dir, "sub/c.log", "ignore this one") // excluded by glob
	}
	got, isErr := runObj(t, CountLines{}, countLinesArgs{Glob: "**/*.txt"}, seed)
	if isErr {
		t.Fatalf("err %s", got)
	}
	m := asMap(t, got)
	if m["lines"].(float64) != 5 || m["words"].(float64) != 6 || m["files"].(float64) != 2 {
		t.Errorf("countlines got lines=%v words=%v files=%v, want 5/6/2", m["lines"], m["words"], m["files"])
	}
	// single file
	got, _ = runObj(t, CountLines{}, countLinesArgs{Path: "a.txt"}, seed)
	if asMap(t, got)["lines"].(float64) != 2 {
		t.Errorf("single-file lines wrong: %s", got)
	}
	// neither path nor glob
	if _, isErr := runObj(t, CountLines{}, countLinesArgs{}, seed); !isErr {
		t.Error("no path/glob should error")
	}
}

func TestGroupBy(t *testing.T) {
	// access log shape: "<status> <bytes>"
	seed := func(dir string) {
		writeFile(dir, "log.txt", "200 10\n200 20\n404 5\n500 1\n200 30\n404 2\n")
	}
	// count by status → 200:3, 404:2, 500:1 (descending by value)
	got, isErr := runObj(t, GroupBy{}, groupByArgs{Path: "log.txt", KeyColumn: 1}, seed)
	if isErr {
		t.Fatalf("count: err %s", got)
	}
	groups := asMap(t, got)["groups"].([]any)
	first := groups[0].(map[string]any)
	if first["key"].(string) != "200" || first["value"].(float64) != 3 {
		t.Errorf("top group = %v, want 200:3", first)
	}
	// sum bytes by status → 200: 60
	got, _ = runObj(t, GroupBy{}, groupByArgs{Path: "log.txt", KeyColumn: 1, Op: "sum", ValueColumn: 2}, seed)
	groups = asMap(t, got)["groups"].([]any)
	if g := groups[0].(map[string]any); g["key"].(string) != "200" || g["value"].(float64) != 60 {
		t.Errorf("top sum group = %v, want 200:60", g)
	}
	// key_pattern capture group
	got, _ = runObj(t, GroupBy{}, groupByArgs{Path: "log.txt", KeyPattern: `^(\d)\d\d`}, seed)
	groups = asMap(t, got)["groups"].([]any)
	if g := groups[0].(map[string]any); g["key"].(string) != "2" || g["value"].(float64) != 3 {
		t.Errorf("pattern top = %v, want 2:3", g)
	}
	// top cap
	got, _ = runObj(t, GroupBy{}, groupByArgs{Path: "log.txt", KeyColumn: 1, Top: 1}, seed)
	out := asMap(t, got)
	if len(out["groups"].([]any)) != 1 || out["groups_truncated"].(bool) != true {
		t.Errorf("top=1 should truncate to 1 group: %s", got)
	}
	// errors
	if _, isErr := runObj(t, GroupBy{}, groupByArgs{Path: "log.txt"}, seed); !isErr {
		t.Error("no key should error")
	}
	if _, isErr := runObj(t, GroupBy{}, groupByArgs{Path: "log.txt", KeyColumn: 1, Op: "sum"}, seed); !isErr {
		t.Error("sum without value_column should error")
	}
}

func TestEmptyFile(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "empty.txt", "") }
	if got, isErr := runObj(t, CountLines{}, countLinesArgs{Path: "empty.txt"}, seed); isErr {
		t.Errorf("empty file should not error: %s", got)
	}
	if got, isErr := runObj(t, Tabulate{}, tabArgs{Path: "empty.txt", Column: 1}, seed); isErr {
		t.Errorf("empty file tabulate should not error: %s", got)
	}
}

// count ignores column, so it must work without one (op=count is filter-driven).
func TestTabulateCountNoColumn(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "d.csv", "1,9\n2,0\n3,9\n") }
	// count rows where col2 > 0 → 2, no `column` supplied
	got, isErr := runObj(t, Tabulate{}, tabArgs{
		Path: "d.csv", Op: "count", Delimiter: ",",
		Filter: &tabFilter{Column: 2, Op: ">", Value: 0},
	}, seed)
	if isErr {
		t.Fatalf("count-no-column errored: %s", got)
	}
	if m := asMap(t, got); m["result"].(float64) != 2 {
		t.Errorf("count where col2>0 = %v, want 2", m["result"])
	}
}

// path and glob are mutually exclusive; supplying both must error, not silently drop glob.
func TestWalkFilesBothPathAndGlob(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "a.txt", "x\n") }
	if _, isErr := runObj(t, CountLines{}, countLinesArgs{Path: "a.txt", Glob: "**/*.txt"}, seed); !isErr {
		t.Error("path+glob together should error")
	}
}

// An optional capture that matches empty must not create a phantom "" group.
func TestGroupByEmptyCaptureSkipped(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "d.txt", "abc\nxyz\n") }
	// (a)? captures "" on the "xyz" line; only "abc" yields key "a"
	got, _ := runObj(t, GroupBy{}, groupByArgs{Path: "d.txt", KeyPattern: `^(a)?`}, seed)
	m := asMap(t, got)
	if m["distinct"].(float64) != 1 {
		t.Errorf("distinct = %v, want 1 (no empty-key group): %s", m["distinct"], got)
	}
	for _, g := range m["groups"].([]any) {
		if g.(map[string]any)["key"].(string) == "" {
			t.Errorf("phantom empty-key group present: %s", got)
		}
	}
}

// A file over the read cap must flag truncated so a partial aggregate is not read as complete.
func TestTabulateTruncatedFlag(t *testing.T) {
	seed := func(dir string) {
		var b strings.Builder
		// each line "1\n" (2 bytes); exceed maxReadBytes (10 MiB)
		for b.Len() <= maxReadBytes+1024 {
			b.WriteString("1\n")
		}
		writeFile(dir, "big.txt", b.String())
	}
	got, isErr := runObj(t, Tabulate{}, tabArgs{Path: "big.txt", Column: 1, Op: "sum"}, seed)
	if isErr {
		t.Fatalf("big file errored: %s", got)
	}
	if m := asMap(t, got); m["truncated"] != true {
		t.Errorf("expected truncated=true for >cap file, got %v", m["truncated"])
	}
}

// The walkFiles (glob) read path must also surface truncation, not just readSingle.
func TestCountLinesTruncatedFlag(t *testing.T) {
	seed := func(dir string) {
		var b strings.Builder
		for b.Len() <= maxReadBytes+1024 {
			b.WriteString("word word word\n")
		}
		writeFile(dir, "big.log", b.String())
	}
	got, isErr := runObj(t, CountLines{}, countLinesArgs{Glob: "**/*.log"}, seed)
	if isErr {
		t.Fatalf("big glob errored: %s", got)
	}
	if m := asMap(t, got); m["truncated"] != true {
		t.Errorf("expected truncated=true via walkFiles glob, got %v", m["truncated"])
	}
}

// NaN/Inf tokens must not be treated as numeric (they would poison sum/min/max).
func TestTabulateRejectsNaNInf(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "d.csv", "5\nNaN\nInf\n7\n") }
	got, isErr := runObj(t, Tabulate{}, tabArgs{Path: "d.csv", Column: 1, Op: "sum"}, seed)
	if isErr {
		t.Fatalf("errored: %s", got)
	}
	m := asMap(t, got)
	if m["result"].(float64) != 12 { // 5+7; NaN/Inf skipped
		t.Errorf("sum = %v, want 12 (NaN/Inf skipped)", m["result"])
	}
	if m["numeric_rows"].(float64) != 2 {
		t.Errorf("numeric_rows = %v, want 2", m["numeric_rows"])
	}
}

// A sum of 0 over an empty/mis-indexed column is distinguishable via numeric_rows:0.
func TestTabulateSumEmptyColumnSignal(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "d.csv", "1,2\n3,4\n") }
	got, _ := runObj(t, Tabulate{}, tabArgs{Path: "d.csv", Column: 9, Op: "sum", Delimiter: ","}, seed)
	m := asMap(t, got)
	if m["result"].(float64) != 0 || m["numeric_rows"].(float64) != 0 {
		t.Errorf("empty column: result=%v numeric_rows=%v, want 0/0", m["result"], m["numeric_rows"])
	}
}

// The 4 aggregate tools must be registered in Default().
func TestAggregateToolsRegistered(t *testing.T) {
	r := Default()
	for _, name := range []string{"tabulate", "countmatches", "countlines", "groupby"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("tool %q not registered in Default()", name)
		}
	}
}
