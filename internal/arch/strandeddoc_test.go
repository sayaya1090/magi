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
// ANOTHER declaration in the same file, with a later line that starts with the declaration's own name.
// That is two docs glued together, and it is how every one of the fifty looked. Types, vars and
// consts strand the same way — thirty more, found when the scan was widened to them. A doc that merely
// mentions a neighbour ("decisionWord is decisionOf plus …") starts with its own name and passes.

// declared is one top-level name in a file and the doc comment Go attaches to it.
type declared struct {
	name string
	doc  *ast.CommentGroup
	pos  token.Pos
}

// declarations lists every function, method, type, var and const a file declares at top level. A
// lone spec with no parentheses has its doc on the GenDecl; a grouped one has it on the spec.
func declarations(f *ast.File) []declared {
	var out []declared
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			out = append(out, declared{d.Name.Name, d.Doc, d.Pos()})
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				var name string
				var doc *ast.CommentGroup
				switch sp := sp.(type) {
				case *ast.TypeSpec:
					name, doc = sp.Name.Name, sp.Doc
				case *ast.ValueSpec:
					if len(sp.Names) != 1 {
						continue
					}
					name, doc = sp.Names[0].Name, sp.Doc
				default:
					continue
				}
				if doc == nil && !d.Lparen.IsValid() {
					doc = d.Doc
				}
				out = append(out, declared{name, doc, sp.Pos()})
			}
		}
	}
	return out
}

// strandedDocs returns "file:line name ← owner" for each doc that carries another declaration's head.
func strandedDocs(name string, src []byte) ([]string, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, name, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	decls := declarations(f)
	known := map[string]bool{}
	for _, d := range decls {
		known[d.name] = true
	}
	var out []string
	for _, d := range decls {
		if d.doc == nil {
			continue
		}
		lines := strings.Split(d.doc.Text(), "\n")
		first := strings.Fields(lines[0])
		if len(first) == 0 || first[0] == d.name || !known[first[0]] {
			continue
		}
		for _, l := range lines[1:] {
			if w := strings.Fields(l); len(w) > 0 && w[0] == d.name {
				out = append(out, name+":"+strconv.Itoa(fs.Position(d.pos).Line)+" "+d.name+" ← "+first[0])
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
	// Not only functions: a type's doc left above a var is the same accident.
	{"package p\n// T is the thing.\n// v counts them.\nvar v int\ntype T struct{}\n", 1},
	// A doc that opens with a word that is not a declaration here is prose, whatever follows.
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
		t.Errorf("%s — the leading paragraph is the doc of the name it opens with: move it back above that declaration", h)
	}
}
