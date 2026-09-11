package builtin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every spawn path that asks for a sandbox token must give it back.
//
// ⚠ **The handle-count guard proves the MECHANISM, and a forgotten call site sails past it.**
// `sandboxProcAttr` is reached from three places — the bash tool, a background job, and a wait-for
// probe — and each one leaks a kernel token per launch if it does not release. Measured 2026-09-11
// on Windows 11 before the fix: two hundred confined launches, handle count up by exactly two
// hundred. Nothing errored, nothing slowed, nothing was logged; the count was the only witness, and
// nobody counts.
//
// Read with go/parser rather than grep, because the thing being asked about is "in the same
// function", which text cannot see — and because a scanner that matches prose bites its own
// explanatory comments, which has happened in this tree more than once.
//
// Deliberately NOT behind a build tag. The leak is Windows-only and the mistake is not: a call site
// added on a Linux machine is a call site that leaks on Windows, and the person adding it should
// hear about it where they are.
func TestEverySandboxTokenIsGivenBack(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The two files that DEFINE the pair name both of them and hold neither obligation.
	defining := map[string]bool{"sandbox_windows.go": true, "sandbox_procattr_other.go": true}

	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || defining[name] {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			t.Fatalf("%s: %v", name, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			asks, releases := calls(fn.Body, "sandboxProcAttr"), calls(fn.Body, "releaseSandbox")
			if !asks {
				return true
			}
			checked++
			if !releases {
				t.Errorf("%s: %s asks for a sandbox token and never releases it — on Windows that is "+
					"one kernel handle per launch, kept until the daemon exits",
					fset.Position(fn.Pos()), fn.Name.Name)
			}
			return true
		})
	}
	if checked < 3 {
		t.Fatalf("only %d spawn paths walked — this guard is reading almost nothing (there are three: "+
			"the bash tool, a background job, a wait-for probe)", checked)
	}
}

// calls reports whether body contains a call to the named function.
func calls(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := c.Fun.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
