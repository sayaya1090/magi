package atomicfile_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Whoever writes with this package must read with it too.
//
// ⚠ **The behaviour this protects cannot be observed where CI runs.** On POSIX a rename is atomic,
// so a reader sees the old file or the new one and os.ReadFile is exactly right; the window that
// needs atomicfile.ReadFile opens only on Windows, where `go test` does not run. Both fixes that
// closed it — config.LoadWithUnknown and experience/git.readFile — are one line each, and
// measured 2026-09-11, reverting either passes gofmt, go vet, GOOS=windows build, GOOS=windows vet
// and the package's own tests on a non-Windows checkout. Every check the tree has, green.
//
// So this asks the one question that IS visible everywhere: which function the call names. It is
// deliberately the weaker claim — calling atomicfile.ReadFile is not the same fact as surviving a
// replacement, and TestAReaderStillNeverSeesHalfAFileWhileItIsReplaced is where that fact lives.
// The pair is the point: the behaviour test proves the effect on the platform that has it, and
// this keeps the call from quietly changing back on the platforms that do not.
//
// Scoped to the readers that were measured losing data, named outright. A list that only checked
// itself would pass just as well with the entries deleted.
func TestTheAtomicWritersReadThroughThisPackageToo(t *testing.T) {
	for _, c := range []struct {
		dir, fn, why string
	}{
		{
			filepath.Join("..", "config"), "LoadWithUnknown",
			"config.toml is replaced by atomicfile.Write and the writers hold a lock the readers " +
				"deliberately do not; 94 of 3000 loads failed while a sibling edited it",
		},
		{
			filepath.Join("..", "adapter", "experience", "git"), "readFile",
			"Pool skips a document whose text reads back empty, so a failed read is a skill that " +
				"silently is not there; 89 of 3000 retrievals lost one",
		},
	} {
		t.Run(c.fn, func(t *testing.T) {
			fn := findFunc(t, c.dir, c.fn)
			var plain, viaPackage int
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "ReadFile" {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok {
					switch id.Name {
					case "os":
						plain++
					case "atomicfile":
						viaPackage++
					}
				}
				return true
			})
			if viaPackage == 0 {
				t.Errorf("%s does not read through atomicfile.ReadFile — %s", c.fn, c.why)
			}
			if plain > 0 {
				t.Errorf("%s still reads with os.ReadFile in %d place(s); on Windows that read "+
					"fails inside the replacement window and the caller cannot tell", c.fn, plain)
			}
		})
	}
}

// findFunc returns the named top-level function's declaration from the package in dir.
func findFunc(t *testing.T, dir, name string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", dir, err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if ok && fn.Recv == nil && fn.Name.Name == name {
					return fn
				}
			}
		}
	}
	t.Fatalf("%s has no top-level func %s — it was renamed or moved, and this guard no longer "+
		"watches anything", dir, name)
	return nil
}
