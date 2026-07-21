package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// End-to-end LSP navigation for Go via the real gopls CLI. Skips when gopls isn't on
// PATH (e.g. CI), so it gives local coverage of runGopls + the Go Execute paths
// without flaking the hermetic build.
func TestLspNavGoplsReal(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	dir := t.TempDir()
	must := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("go.mod", "module example.com/x\n\ngo 1.21\n")
	must("a.go", "package x\n\nfunc Foo() int { return Bar() }\n\nfunc Bar() int { return 1 }\n")
	ctx := context.Background()
	env := port.ToolEnv{Workdir: dir}
	run := func(tl port.Tool, args string) (string, bool) {
		r, _ := tl.Execute(ctx, json.RawMessage(args), env)
		return resultText(t, r), r.IsError
	}

	// Outline lists the file's functions.
	if out, isErr := run(Lsp{}, `{"kind":"symbols","path":"a.go"}`); isErr || !strings.Contains(out, "Foo") || !strings.Contains(out, "Bar") {
		t.Errorf("lsp symbols = %q (err=%v)", out, isErr)
	}
	// Definition of Bar (used on line 3) resolves to its declaration (line 5).
	if out, isErr := run(Lsp{}, `{"kind":"definition","path":"a.go","line":3,"symbol":"Bar"}`); isErr || !strings.Contains(out, "a.go:5") {
		t.Errorf("lsp definition = %q (err=%v), want a.go:5", out, isErr)
	}
	// References of Foo include its declaration line.
	if out, isErr := run(Lsp{}, `{"kind":"references","path":"a.go","line":3,"symbol":"Foo"}`); isErr || !strings.Contains(out, "a.go:3") {
		t.Errorf("lsp references = %q (err=%v)", out, isErr)
	}
}
