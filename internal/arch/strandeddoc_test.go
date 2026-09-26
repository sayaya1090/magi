package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A doc comment belongs to the declaration directly under it, so a function inserted between a doc
// and its function takes that doc over. Nothing complains: the stranded paragraph now opens the
// intruder's doc, the owner has none, and godoc shows each of them the wrong text. Found once in
// jsonx (fourteen lines of two other functions' docs, cut mid-sentence, in front of
// StripTrailingCommas) and then fifty more times across the tree by the scan below.
//
// The shape it looks for is narrow on purpose: a function's doc whose first word is the name of
// ANOTHER function in the same file, with a later line that starts with the function's own name.
// That is two docs glued together, and it is how every one of the fifty looked. A doc that merely
// mentions a neighbour ("decisionWord is decisionOf plus …") starts with its own name and passes.

// strandedDocs returns "file:line fn ← owner" for each doc that carries another function's head.
func strandedDocs(name string, src []byte) ([]string, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, name, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	funcs := map[string]bool{}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			funcs[fd.Name.Name] = true
		}
	}
	var out []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Doc == nil {
			continue
		}
		lines := strings.Split(fd.Doc.Text(), "\n")
		first := strings.Fields(lines[0])
		if len(first) == 0 || first[0] == fd.Name.Name || !funcs[first[0]] {
			continue
		}
		for _, l := range lines[1:] {
			if w := strings.Fields(l); len(w) > 0 && w[0] == fd.Name.Name {
				out = append(out, name+":"+
					strconv.Itoa(fs.Position(fd.Doc.Pos()).Line)+" "+fd.Name.Name+" ← "+first[0])
				break
			}
		}
	}
	return out, nil
}

// strandedSamples pins what the scan must and must not flag, so a scan that stopped seeing cannot
// pass as a clean tree.
var strandedSamples = []struct {
	src  string
	want int
}{
	// The shape itself: b's doc was left above a, which was inserted after it.
	{"package p\n// b does the second thing.\n// a does the first thing.\nfunc a() {}\nfunc b() {}\n", 1},
	// Mentioning a neighbour is not stranding: the doc starts with its own name.
	{"package p\n// a is b plus a check.\nfunc a() {}\nfunc b() {}\n", 0},
	// A doc that opens with a word that is not a function here is prose, whatever follows.
	{"package p\n// The b case.\n// a does it.\nfunc a() {}\nfunc b() {}\n", 0},
}

func TestNoStrandedDocComments(t *testing.T) {
	for _, s := range strandedSamples {
		got, err := strandedDocs("sample.go", []byte(s.src))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != s.want {
			t.Fatalf("the scan is broken, so a clean result below would mean nothing: %d hit(s) %v, want %d, in\n%s",
				len(got), got, s.want, s.src)
		}
	}
	var hits []string
	for _, f := range goFiles(t) {
		src, err := os.ReadFile(filepath.Join("..", "..", f))
		if err != nil {
			t.Fatal(err)
		}
		h, err := strandedDocs(f, src)
		if err != nil {
			continue // not this test's business; the build reports it
		}
		hits = append(hits, h...)
	}
	for _, h := range hits {
		t.Errorf("%s — the leading paragraph is the second function's doc: move it back above that function", h)
	}
}
