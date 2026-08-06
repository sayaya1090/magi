package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The screen reaches the engine ONLY through the interface.
//
// The point of writing the boundary down is lost the moment something calls straight into
// *app.App again: the interface would still compile, still be satisfied, and still describe
// something other than what the UI actually needs. This is the check that keeps the two the same
// thing — and it is a source check because the type system cannot state it.
func TestTheScreenTouchesTheEngineOnlyThroughTheInterface(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// engine.go is where the in-process implementation is named on purpose.
	bad := map[string][]string{}
	for _, f := range files {
		if f == "engine.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "app.App") {
				bad[f] = append(bad[f], strings.TrimSpace(line)+"  (line "+itoa(i+1)+")")
			}
		}
	}
	if len(bad) > 0 {
		for f, ls := range bad {
			t.Errorf("%s names *app.App directly — the boundary is engine.Engine:\n  %s",
				f, strings.Join(ls, "\n  "))
		}
	}
}

// Every method the interface declares is one the screen actually calls.
//
// An interface that grows past its use is the thing it was made to prevent: a second
// implementation would owe methods nobody wants, and the count that makes the boundary worth
// discussing would be a fiction.
func TestTheInterfaceDeclaresNothingTheScreenDoesNotUse(t *testing.T) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "engine.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	ast.Inspect(af, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Engine" {
			return true
		}
		it, ok := ts.Type.(*ast.InterfaceType)
		if !ok {
			return false
		}
		for _, m := range it.Methods.List {
			for _, nm := range m.Names {
				declared = append(declared, nm.Name)
			}
		}
		return false
	})
	if len(declared) == 0 {
		t.Fatal("engine.go declares no Engine methods")
	}

	// What the package actually calls, from every file but engine.go.
	body := &strings.Builder{}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if f == "engine.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		body.Write(b)
	}
	// Match a call ON THE ENGINE, not any `.Method(` in the package. `.Close(` matches a file and
	// a listener too, and a check that loose reports every declared method as used — which is the
	// one outcome that makes this test worthless.
	//
	// The engine is reached under three spellings today: the model's field and the two parameter
	// names New and Run give it. A fourth spelling makes this report a false positive, which is the
	// safe direction: it fails loudly and someone adds the name here.
	src := body.String()
	recv := `(?:m\.app|\ba|\bapp)`
	for _, m := range declared {
		if !regexp.MustCompile(recv + `\.` + m + `\(`).MatchString(src) {
			t.Errorf("Engine declares %s, which nothing in the package calls on the engine", m)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
