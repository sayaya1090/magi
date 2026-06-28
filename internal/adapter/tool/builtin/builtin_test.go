package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"net/http"
	"net/http/httptest"

	"github.com/sayaya1090/magi/internal/port"
)

// run executes a tool with JSON args in a fresh temp workdir seeded by setup.
func run(t *testing.T, tool port.Tool, args any, setup func(dir string)) (string, bool) {
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
	var content string
	_ = json.Unmarshal(res.Content, &content)
	return content, res.IsError
}

func writeFile(dir, rel, content string) {
	p := filepath.Join(dir, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(content), 0o644)
}

// ---- F-TOOL-READ ----
func TestRead(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "a.txt", "hello\nworld\n") }

	if got, isErr := run(t, Read{}, readArgs{Path: "a.txt"}, seed); isErr || got != "     1\thello\n     2\tworld\n" {
		t.Errorf("read-1: got %q err=%v", got, isErr)
	}
	if got, isErr := run(t, Read{}, readArgs{Path: "a.txt", Offset: 2, Limit: 1}, seed); isErr || got != "     2\tworld\n" {
		t.Errorf("read-2: got %q err=%v", got, isErr)
	}
	if _, isErr := run(t, Read{}, readArgs{Path: "nope.txt"}, nil); !isErr {
		t.Errorf("read-3: expected error for missing file")
	}
	if _, isErr := run(t, Read{}, readArgs{Path: "sub"}, func(d string) { os.Mkdir(filepath.Join(d, "sub"), 0o755) }); !isErr {
		t.Errorf("read-4: expected error for directory")
	}
	if _, isErr := run(t, Read{}, readArgs{Path: "img.bin"}, func(d string) { writeFile(d, "img.bin", "ab\x00cd") }); !isErr {
		t.Errorf("read-5: expected error for binary file")
	}
	if _, isErr := run(t, Read{}, readArgs{Path: "/etc/passwd"}, nil); !isErr {
		t.Errorf("read-6: expected error for path outside workdir")
	}
}

// read-locate: an imprecise path (right name, wrong dir) is recovered by reading
// the unique same-named file in the tree, with a note — so agents don't dead-end.
func TestReadLocatesByBasename(t *testing.T) {
	seed := func(dir string) { writeFile(dir, "docs/DESIGN.md", "spec\n") }
	got, isErr := run(t, Read{}, readArgs{Path: "DESIGN.md"}, seed)
	if isErr {
		t.Fatalf("read-locate: expected recovery, got error %q", got)
	}
	if !strings.Contains(got, filepath.Join("docs", "DESIGN.md")) || !strings.Contains(got, "spec") {
		t.Errorf("read-locate: expected note + content, got %q", got)
	}
}

// read-locate ambiguous: multiple same-named files → suggest, don't guess.
func TestReadSuggestsOnAmbiguity(t *testing.T) {
	seed := func(dir string) {
		writeFile(dir, "a/conf.yaml", "x")
		writeFile(dir, "b/conf.yaml", "y")
	}
	got, isErr := run(t, Read{}, readArgs{Path: "conf.yaml"}, seed)
	if !isErr || !strings.Contains(got, "did you mean") {
		t.Errorf("read-locate ambiguous: expected suggestion error, got %q err=%v", got, isErr)
	}
}

// ---- F-TOOL-WRITE ----
func TestWrite(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal(writeArgs{Path: "new.txt", Content: "hi"})
	res, _ := Write{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if res.IsError {
		t.Fatalf("write-1: unexpected error")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "new.txt")); string(b) != "hi" {
		t.Errorf("write-1: file content = %q, want hi", b)
	}

	raw, _ = json.Marshal(writeArgs{Path: "x/y/z.txt", Content: "a"})
	res, _ = Write{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if res.IsError {
		t.Fatalf("write-2: unexpected error")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "x/y/z.txt")); string(b) != "a" {
		t.Errorf("write-2: nested file content = %q, want a", b)
	}

	writeFile(dir, "old.txt", "old")
	raw, _ = json.Marshal(writeArgs{Path: "old.txt", Content: "new"})
	Write{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if b, _ := os.ReadFile(filepath.Join(dir, "old.txt")); string(b) != "new" {
		t.Errorf("write-3: overwrite content = %q, want new", b)
	}

	if _, isErr := run(t, Write{}, writeArgs{Path: "../escape.txt", Content: "x"}, nil); !isErr {
		t.Errorf("write-4: expected error for path outside workdir")
	}
}

// ---- F-TOOL-EDIT ----
func TestEdit(t *testing.T) {
	cases := []struct {
		name    string
		initial string
		args    editArgs
		want    string // expected file content after edit (only when !wantErr)
		wantErr bool
	}{
		{"edit-1", "foo bar baz", editArgs{Path: "f", Old: "bar", New: "BAR"}, "foo BAR baz", false},
		{"edit-2", "x x x", editArgs{Path: "f", Old: "x", New: "y"}, "", true},
		{"edit-3", "x x x", editArgs{Path: "f", Old: "x", New: "y", ReplaceAll: true}, "y y y", false},
		{"edit-4", "abc", editArgs{Path: "f", Old: "zzz", New: "y"}, "", true},
		{"edit-5", "abc", editArgs{Path: "f", Old: "abc", New: "abc"}, "", true},
		{"edit-6", "a\r\nb", editArgs{Path: "f", Old: "a", New: "A"}, "A\r\nb", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(dir, "f", tc.initial)
			raw, _ := json.Marshal(tc.args)
			res, _ := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
			if res.IsError != tc.wantErr {
				t.Fatalf("%s: isErr=%v want %v", tc.name, res.IsError, tc.wantErr)
			}
			if !tc.wantErr {
				if b, _ := os.ReadFile(filepath.Join(dir, "f")); string(b) != tc.want {
					t.Errorf("%s: content=%q want %q", tc.name, b, tc.want)
				}
			}
		})
	}
}

// ---- F-TOOL-GREP ----
func TestGrep(t *testing.T) {
	got, isErr := runJSON(t, Grep{}, grepArgs{Pattern: "foo"}, func(d string) {
		writeFile(d, "a.txt", "foo\nbar\nfoobar")
	})
	if isErr {
		t.Fatalf("grep-1: unexpected error")
	}
	want := []any{"a.txt:1:foo", "a.txt:3:foobar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("grep-1: got %v want %v", got, want)
	}

	got, _ = runJSON(t, Grep{}, grepArgs{Pattern: "foo", Glob: "*.txt"}, func(d string) {
		writeFile(d, "a.txt", "foo")
		writeFile(d, "b.go", "foo")
	})
	if !reflect.DeepEqual(got, []any{"a.txt:1:foo"}) {
		t.Errorf("grep-2: got %v want [a.txt:1:foo]", got)
	}

	got, isErr = runJSON(t, Grep{}, grepArgs{Pattern: "zzz"}, func(d string) { writeFile(d, "a.txt", "foo") })
	if isErr || len(got) != 0 {
		t.Errorf("grep-3: got %v err=%v, want empty", got, isErr)
	}

	if _, isErr := run(t, Grep{}, grepArgs{Pattern: "[("}, nil); !isErr {
		t.Errorf("grep-4: expected error for invalid regex")
	}
}

// ---- F-TOOL-GLOB ----
func TestGlob(t *testing.T) {
	got, _ := runJSON(t, Glob{}, globArgs{Pattern: "*.go"}, func(d string) {
		writeFile(d, "a.go", "")
		writeFile(d, "b.go", "")
		writeFile(d, "c.txt", "")
	})
	if !reflect.DeepEqual(got, []any{"a.go", "b.go"}) {
		t.Errorf("glob-1: got %v want [a.go b.go]", got)
	}

	got, _ = runJSON(t, Glob{}, globArgs{Pattern: "src/**/*.go"}, func(d string) {
		writeFile(d, "src/x.go", "")
		writeFile(d, "src/sub/y.go", "")
	})
	if !reflect.DeepEqual(got, []any{"src/sub/y.go", "src/x.go"}) {
		t.Errorf("glob-2: got %v want [src/sub/y.go src/x.go]", got)
	}

	got, _ = runJSON(t, Glob{}, globArgs{Pattern: "*.md"}, func(d string) { writeFile(d, "a.txt", "") })
	if len(got) != 0 {
		t.Errorf("glob-3: got %v want []", got)
	}
}

// ---- F-TOOL-LIST ----
func TestList(t *testing.T) {
	got, isErr := runJSON(t, List{}, listArgs{Path: "dir"}, func(d string) {
		writeFile(d, "dir/b.txt", "")
		writeFile(d, "dir/c.txt", "")
		os.MkdirAll(filepath.Join(d, "dir/a"), 0o755)
	})
	if isErr {
		t.Fatalf("list-1: unexpected error")
	}
	// Expect directory "a" first, then b.txt, c.txt.
	names := make([]string, len(got))
	for i, e := range got {
		m := e.(map[string]any)
		names[i] = m["name"].(string)
	}
	if !reflect.DeepEqual(names, []string{"a", "b.txt", "c.txt"}) {
		t.Errorf("list-1: order=%v want [a b.txt c.txt]", names)
	}

	if _, isErr := run(t, List{}, listArgs{Path: "nope"}, nil); !isErr {
		t.Errorf("list-2: expected error for missing dir")
	}
}

// runJSON executes a tool and decodes its JSON-array result.
func runJSON(t *testing.T, tool port.Tool, args any, setup func(dir string)) ([]any, bool) {
	t.Helper()
	dir := t.TempDir()
	if setup != nil {
		setup(dir)
	}
	raw, _ := json.Marshal(args)
	res, err := tool.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		return nil, true
	}
	var out []any
	if err := json.Unmarshal(res.Content, &out); err != nil {
		t.Fatalf("decode result: %v (content=%s)", err, res.Content)
	}
	return out, false
}

// ---- Registry ----
func TestDefaultRegistry(t *testing.T) {
	r := Default()
	for _, name := range []string{"read", "write", "edit", "multiedit", "grep", "glob", "list", "bash", "bash_output", "bash_kill", "todowrite", "webfetch", "websearch", "remember", "skill", "findcontext", "astgrep", "lsp_diagnostics", "lsp_definition", "lsp_references", "lsp_symbols"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("default registry missing tool %q", name)
		}
	}
	if len(r.List()) != 21 {
		t.Errorf("registry size = %d, want 21 (added websearch)", len(r.List()))
	}
}

// F-TOOL webfetch: fetches a URL and strips HTML to text.
func TestWebFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><head><style>x{}</style></head><body><h1>Title</h1><p>Hello &amp; world</p><script>bad()</script></body></html>"))
	}))
	defer srv.Close()

	raw, _ := json.Marshal(webFetchArgs{URL: srv.URL})
	res, _ := WebFetch{}.Execute(context.Background(), raw, port.ToolEnv{})
	var out string
	_ = json.Unmarshal(res.Content, &out)
	if res.IsError {
		t.Fatalf("webfetch: unexpected error: %s", out)
	}
	if !strings.Contains(out, "Title") || !strings.Contains(out, "Hello & world") {
		t.Errorf("webfetch text=%q", out)
	}
	if strings.Contains(out, "bad()") || strings.Contains(out, "x{}") {
		t.Errorf("script/style not stripped: %q", out)
	}
	// External content is fenced as untrusted (prompt-injection mitigation).
	if !strings.Contains(out, "BEGIN UNTRUSTED") || !strings.Contains(out, "END UNTRUSTED") {
		t.Errorf("webfetch output should be fenced as untrusted: %q", out)
	}

	// Non-http URL rejected.
	raw, _ = json.Marshal(webFetchArgs{URL: "file:///etc/passwd"})
	res, _ = WebFetch{}.Execute(context.Background(), raw, port.ToolEnv{})
	if !res.IsError {
		t.Error("webfetch: expected error for non-http url")
	}
}

// F-TOOL multiedit: all-or-nothing application.
func TestMultiEdit(t *testing.T) {
	// All hunks valid → applied.
	dir := t.TempDir()
	writeFile(dir, "f", "foo bar baz")
	raw, _ := json.Marshal(multiEditArgs{Path: "f", Edits: []editHunk{
		{Old: "foo", New: "FOO"}, {Old: "baz", New: "BAZ"},
	}})
	res, _ := MultiEdit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if res.IsError {
		t.Fatalf("multiedit ok: unexpected error")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f")); string(b) != "FOO bar BAZ" {
		t.Errorf("content=%q want 'FOO bar BAZ'", b)
	}

	// One bad hunk → nothing written (atomic).
	dir2 := t.TempDir()
	writeFile(dir2, "f", "foo bar")
	raw, _ = json.Marshal(multiEditArgs{Path: "f", Edits: []editHunk{
		{Old: "foo", New: "FOO"}, {Old: "zzz", New: "X"}, // second fails
	}})
	res, _ = MultiEdit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir2})
	if !res.IsError {
		t.Errorf("multiedit atomic: expected error")
	}
	if b, _ := os.ReadFile(filepath.Join(dir2, "f")); string(b) != "foo bar" {
		t.Errorf("atomic: file must be unchanged, got %q", b)
	}
}

// F-TOOL bash: runs a command and reports exit code + output.
func TestBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	dir := t.TempDir()
	raw, _ := json.Marshal(bashArgs{Command: "echo hello && exit 0"})
	res, _ := Bash{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	var out string
	_ = json.Unmarshal(res.Content, &out)
	if res.IsError || !strings.Contains(out, "hello") || !strings.Contains(out, "exit 0") {
		t.Errorf("bash ok: out=%q isErr=%v", out, res.IsError)
	}

	raw, _ = json.Marshal(bashArgs{Command: "exit 3"})
	res, _ = Bash{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	_ = json.Unmarshal(res.Content, &out)
	if !res.IsError || !strings.Contains(out, "exit 3") {
		t.Errorf("bash nonzero: out=%q isErr=%v", out, res.IsError)
	}

	// Runs in the workdir.
	writeFile(dir, "marker.txt", "x")
	raw, _ = json.Marshal(bashArgs{Command: "ls"})
	res, _ = Bash{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	_ = json.Unmarshal(res.Content, &out)
	if !strings.Contains(out, "marker.txt") {
		t.Errorf("bash workdir: out=%q", out)
	}
}
