package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Scheduled jobs must be started by a daemon and by nothing else.
//
// The reason is not tidiness. Three terminals open in one repository would be three companions all
// running the nightly audit, against the same files, at the same second — and the failure would
// look like a flaky job rather than like three of them. There is no runtime guard for it: RunCron
// runs whatever it is given, wherever it is called. The guarantee is the call site, so the call
// site is what this checks.
//
// Read from the syntax tree rather than by grepping, because the question is structural — is this
// call inside the daemon branch — and a grep can only ask whether two strings appear near each
// other. A refactor that moved the call out of the branch while leaving the text nearby would go
// unnoticed.
func TestOnlyADaemonStartsScheduledWork(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	type site struct {
		file    string
		line    int
		guarded bool
	}
	var sites []site

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		// Two passes over position ranges rather than a stack maintained during one walk. The stack
		// version of this was wrong and said so loudly: ast.Inspect calls back with a nil node when
		// it leaves ANY node, not only the ones that were pushed, so the depth drifted and a call
		// plainly inside the daemon branch was reported as outside it. Ranges cannot drift.
		var guards []*ast.IfStmt
		var calls []*ast.CallExpr
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.IfStmt:
				if mentions(x.Cond, "daemonMode") {
					guards = append(guards, x)
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "RunCron" {
					calls = append(calls, x)
				}
			}
			return true
		})
		for _, call := range calls {
			guarded := false
			for _, g := range guards {
				// The body only. A call in the else branch of "if *daemonMode" is a call in every
				// interactive session, which is the thing being forbidden.
				if g.Body != nil && call.Pos() >= g.Body.Pos() && call.End() <= g.Body.End() {
					guarded = true
					break
				}
			}
			sites = append(sites, site{name, fset.Position(call.Pos()).Line, guarded})
		}
	}

	if len(sites) == 0 {
		t.Fatal("nothing starts the scheduler: no call to RunCron in cmd/magi")
	}
	if len(sites) > 1 {
		t.Errorf("RunCron is called from %d places; scheduled work must have exactly one start: %+v",
			len(sites), sites)
	}
	for _, s := range sites {
		if !s.guarded {
			t.Errorf("%s:%d calls RunCron outside a daemonMode branch — every interactive session in "+
				"this workspace would fire the same jobs", s.file, s.line)
		}
	}
}

// mentions reports whether an expression uses the named identifier, however it is wrapped: the
// condition may be a bare *daemonMode today and something like *daemonMode && !*noCron tomorrow.
func mentions(e ast.Expr, name string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
