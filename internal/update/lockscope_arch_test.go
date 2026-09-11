package update

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// Everything that changes an install's transaction state takes the install lock first.
//
// ⚠ **The lock existed and only ONE of six callers used it.** `Commit` took it; `Salvage`, `Resume`,
// `Confirm`, `Retry` and `LeftCleanly` did not (review R10), so the file that says which build is
// recoverable could be rewritten by one process while another was restoring from it. Proved with two
// processes in `lockscope_test.go`: a Salvage undid a live Commit and deleted the backup that
// Commit's own rollback depends on.
//
// A per-function guard rather than one more case in that test, because the failure mode is a SIXTH
// caller added later — and the two-process test only exercises the one path somebody thought to
// write. This asks the question of every function in the package at once.
//
// Read with go/parser, not grep: "in the same function" is not something text can see, and a scanner
// that matches prose bites the comments explaining it — which has happened in this tree before.
//
// Deliberately unguarded by a build tag. The lock has a per-platform implementation but the RULE is
// platform-neutral, and a caller added on a machine that never runs the Windows path is still a
// caller that has to obey it.
func TestEveryTransactionStepTakesTheInstallLock(t *testing.T) {
	// Writing the ledger, restoring the backup, or deleting it — the three ways to move a
	// transaction. Anything exported that does one of these is arbitrating over the install.
	changesState := map[string]bool{"writeLedger": true, "restorePrevious": true, "removePrev": true,
		// `Began` reaches the ledger through the lock-free inner form, so name that too — otherwise
		// the one function whose whole job is opening a transaction would not be walked at all.
		"beganHeld": true}
	// Commit takes the lock ITSELF, and non-blocking on purpose: an updater that cannot get it says
	// so and carries on rather than queueing (CLIENT_LIFECYCLE §9.3). It reaches the ledger through
	// `beganHeld`, which is unexported precisely so it cannot be called from outside the lock.
	takesItsOwn := map[string]bool{"Commit": true}

	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	files := pkg["update"]
	if files == nil {
		t.Fatal("package update did not parse — this guard is reading nothing")
	}

	var checked []string
	for name, f := range files.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !fn.Name.IsExported() || fn.Recv != nil {
				return true
			}
			if !callsAny(fn.Body, changesState) {
				return true
			}
			checked = append(checked, fn.Name.Name)
			if takesItsOwn[fn.Name.Name] {
				if !callsAny(fn.Body, map[string]bool{"holdInstall": true}) {
					t.Errorf("%s (%s) is supposed to take the lock itself and does not", fn.Name.Name, name)
				}
				return true
			}
			if !callsAny(fn.Body, map[string]bool{"takeInstall": true}) {
				t.Errorf("%s (%s) moves an install's transaction without taking the install lock — "+
					"another process can be replacing the very files it is reading and writing",
					fn.Name.Name, fset.Position(fn.Pos()))
			}
			return true
		})
	}

	sort.Strings(checked)
	want := []string{"Began", "Confirm", "LeftCleanly", "Resume", "Retry", "Salvage"}
	if len(checked) < len(want) {
		t.Fatalf("only %v walked — this guard is reading almost nothing; it should see at least %v",
			checked, want)
	}
	for _, w := range want {
		if sort.SearchStrings(checked, w) >= len(checked) || checked[sort.SearchStrings(checked, w)] != w {
			t.Errorf("%s was not among the functions walked (%v) — it no longer changes transaction "+
				"state, or this guard stopped seeing it", w, checked)
		}
	}
}

// callsAny reports whether body calls any of the named functions.
func callsAny(body *ast.BlockStmt, names map[string]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := c.Fun.(*ast.Ident); ok && names[id.Name] {
			found = true
		}
		return !found
	})
	return found
}
