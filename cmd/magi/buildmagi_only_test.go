package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// One place decides what a magi executable is called.
//
// ⚠ **A second place decided it wrong, and the guard it broke was the one that proves a feature name
// is not a lie.** `features_live_test.go` built its own binary at `<dir>/magi`, and on Windows a file
// with no extension is not executable at all — `exec.Command` answers "executable file not found in
// %PATH%" for a file it is looking straight at. So
// `TestEveryAdvertisedFeatureIsOneThisBinaryActuallyHas` has failed on every Windows run since it was
// written, which means the floor under `--features` was never actually measured there.
//
// `buildMagi` had the suffix all along, AND a check that the thing it built can be started — a
// fixture that cannot produce a runnable binary says so about itself rather than letting the next
// assertion phrase it as a defect in the product. This keeps the second copy from coming back.
//
// go/parser rather than grep: the call is what matters, and a text scan would match this comment.
func TestOnlyBuildMagiBuildsMagi(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), "_test.go") && fi.Name() != "buildmagi_test.go"
	}, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			seen++
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Command" {
					return true
				}
				if pkgName, ok := sel.X.(*ast.Ident); !ok || pkgName.Name != "exec" {
					return true
				}
				if lit(call.Args[0]) != "go" || lit(call.Args[1]) != "build" {
					return true
				}
				t.Errorf("%s builds magi itself — use buildMagi(t, dir), which is where the "+
					"platform's executable suffix lives and which checks that what it built can "+
					"actually be started (%s)", filepath.Base(name), fset.Position(call.Pos()))
				return true
			})
		}
	}
	if seen < 5 {
		t.Fatalf("only %d test files walked — this guard is reading almost nothing", seen)
	}
}

// lit is the string value of a basic literal argument, or "" for anything else.
func lit(e ast.Expr) string {
	b, ok := e.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return ""
	}
	return strings.Trim(b.Value, `"`)
}
