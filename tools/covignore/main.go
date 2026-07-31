// covignore drops the coverage blocks of functions marked `//coverage:ignore`.
//
// Go's cover profile counts every statement that compiles, including the ones no test can
// reach: the process entry point, a one-line adapter that exists to satisfy an interface, a
// sink whose whole contract is to do nothing. Those pull the number down without naming
// anything worth writing, and a number that is wrong in a known direction stops being read.
//
// The exclusion is per FUNCTION and written at the function, not in a build script, so it is
// visible to whoever is looking at the code and shows up in review when it grows. It also
// carries a reason — the marker without one is rejected — because "no test target" is a claim
// about the function, and an unexplained claim is the kind that quietly spreads to code that
// simply has not been tested yet.
//
// Two guards keep the list honest, both of them errors:
//
//   - A marked function with COVERED statements is a lie: something does exercise it, so the
//     marker is either stale or was wrong. (Only complete non-coverage is consistent with
//     "nothing can call this".)
//   - A marker on a function whose blocks are not in the profile at all means the name drifted.
//
// Usage:
//
//	covignore -i coverage.out -o coverage.prod.out
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// marker is the directive form (no space after //), so gofmt leaves it alone and it reads as
// what it is: an instruction to a tool, not prose.
const marker = "//coverage:ignore"

// span is a half-open line range [lo, hi] (inclusive both ends — a profile block is reported
// in the same terms) belonging to one ignored function.
type span struct {
	lo, hi int
	name   string
	reason string
}

// result is what one filtering pass produced: the profile to write, the per-function note for
// whoever is reading the build log, and the problems that make the pass a failure. It is a
// value rather than direct printing so the guards can be tested — they are the whole reason
// this tool is allowed to change a number other people read.
type result struct {
	kept     []string
	notes    []string
	problems []string
}

//coverage:ignore flags, files and os.Exit around filterProfile, which is what is tested
func main() {
	in := flag.String("i", "coverage.out", "coverage profile to filter")
	out := flag.String("o", "", "where to write the filtered profile (default: stdout)")
	root := flag.String("root", ".", "module root, for resolving profile paths to source files")
	flag.Parse()

	mod, err := modulePath(filepath.Join(*root, "go.mod"))
	if err != nil {
		fail(err)
	}
	lines, err := readLines(*in)
	if err != nil {
		fail(err)
	}
	if len(lines) == 0 {
		fail(fmt.Errorf("%s is empty", *in))
	}

	res, err := filterProfile(lines, *root, mod)
	if err != nil {
		fail(err)
	}
	// Report before failing: a stale marker is easier to fix when you can see the whole list.
	for _, n := range res.notes {
		fmt.Fprintln(os.Stderr, "covignore: "+n)
	}
	for _, p := range res.problems {
		fmt.Fprintln(os.Stderr, "covignore: ERROR "+p)
	}
	if len(res.problems) > 0 {
		os.Exit(1)
	}

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		w = f
	}
	for _, l := range res.kept {
		fmt.Fprintln(w, l)
	}
}

// filterProfile drops the blocks belonging to marked functions and reports on what it did.
// lines is a whole profile, first line included.
func filterProfile(lines []string, root, mod string) (result, error) {
	var res result

	// Every file the profile mentions, parsed once. A file with no marker contributes nothing
	// and costs one parse; the profile names only files that compiled, so none of this can be
	// looking at generated or vendored trees.
	ignored := map[string][]span{}
	seen := map[string]bool{}
	for _, l := range lines[1:] {
		file, _, _, ok := parseBlock(l)
		if !ok || seen[file] {
			continue
		}
		seen[file] = true
		rel, ok := strings.CutPrefix(file, mod+"/")
		if !ok {
			continue // a dependency's file: not ours to mark
		}
		spans, err := markedFuncs(filepath.Join(root, rel))
		if err != nil {
			return res, err
		}
		if len(spans) > 0 {
			ignored[file] = spans
		}
	}

	res.kept = []string{lines[0]}
	blocks := map[string]int{}  // "file:func" → profile blocks matched (0 means the marker drifted)
	dropped := map[string]int{} // …statements in them
	covered := map[string]int{} // …of which were actually EXERCISED (a broken marker)
	for _, l := range lines[1:] {
		file, lo, hi, ok := parseBlock(l)
		if !ok {
			res.kept = append(res.kept, l)
			continue
		}
		s := find(ignored[file], lo, hi)
		if s == nil {
			res.kept = append(res.kept, l)
			continue
		}
		key := file + ":" + s.name
		stmts, hits := statementsAndHits(l)
		// Counted separately from the statements: an empty sink is a real block holding zero
		// of them, and reading "no statements" as "no blocks" reported every do-nothing method
		// — the clearest case the marker exists for — as a marker that had drifted.
		blocks[key]++
		dropped[key] += stmts
		covered[key] += hits
	}

	var keys []string
	for k := range blocks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		res.notes = append(res.notes, fmt.Sprintf("%s (%d statements)", k, dropped[k]))
	}
	for _, k := range keys {
		if covered[k] > 0 {
			res.problems = append(res.problems, fmt.Sprintf(
				"%s is marked unreachable but %d of its statements RAN — something tests it, so drop the marker",
				k, covered[k]))
		}
	}
	var stale []string
	for file, spans := range ignored {
		for _, s := range spans {
			if blocks[file+":"+s.name] == 0 {
				stale = append(stale, fmt.Sprintf(
					"%s:%s is marked but has no blocks in the profile — the marker is on something the profile does not name",
					file, s.name))
			}
		}
	}
	sort.Strings(stale) // map order is not an order; the build log should read the same twice
	res.problems = append(res.problems, stale...)
	return res, nil
}

// markedFuncs returns the line span of every top-level func and method in path whose doc
// comment carries the marker. A marker with no reason after it is rejected: the reason is the
// only thing that distinguishes "nothing can reach this" from "nobody got around to it".
func markedFuncs(path string) ([]span, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []span
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Doc == nil {
			continue
		}
		for _, c := range fn.Doc.List {
			reason, ok := strings.CutPrefix(c.Text, marker)
			if !ok {
				continue
			}
			reason = strings.TrimSpace(reason)
			if reason == "" {
				return nil, fmt.Errorf("%s: %s needs a reason after %s",
					fset.Position(c.Pos()), name(fn), marker)
			}
			out = append(out, span{
				lo:     fset.Position(fn.Pos()).Line,
				hi:     fset.Position(fn.End()).Line,
				name:   name(fn),
				reason: reason,
			})
			break
		}
	}
	return out, nil
}

// name renders a func or method the way the cover tool does: Recv.Method, or Func.
func name(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// find returns the ignored span containing a block, or nil. A block belongs to the function
// whose braces enclose it; the profile's own line numbers are all that is needed.
func find(spans []span, lo, hi int) *span {
	for i := range spans {
		if lo >= spans[i].lo && hi <= spans[i].hi {
			return &spans[i]
		}
	}
	return nil
}

// parseBlock pulls the file and line range out of a profile line, which reads
// "path/file.go:startLine.startCol,endLine.endCol numStmt count".
func parseBlock(l string) (file string, lo, hi int, ok bool) {
	colon := strings.LastIndex(l, ":")
	sp := strings.Index(l, " ")
	if colon < 0 || sp < 0 || colon > sp {
		return "", 0, 0, false
	}
	rng := l[colon+1 : sp]
	comma := strings.Index(rng, ",")
	if comma < 0 {
		return "", 0, 0, false
	}
	lo, err1 := strconv.Atoi(firstField(rng[:comma], '.'))
	hi, err2 := strconv.Atoi(firstField(rng[comma+1:], '.'))
	if err1 != nil || err2 != nil {
		return "", 0, 0, false
	}
	return l[:colon], lo, hi, true
}

// statementsAndHits reports how many statements a profile block holds, and how many of them
// the run actually executed (0 or all of them — a block is atomic).
func statementsAndHits(l string) (stmts, hits int) {
	f := strings.Fields(l)
	if len(f) < 3 {
		return 0, 0
	}
	stmts, _ = strconv.Atoi(f[len(f)-2])
	count, _ := strconv.Atoi(f[len(f)-1])
	if count > 0 {
		return stmts, stmts
	}
	return stmts, 0
}

func firstField(s string, sep byte) string {
	if i := strings.IndexByte(s, sep); i >= 0 {
		return s[:i]
	}
	return s
}

func modulePath(gomod string) (string, error) {
	b, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("%s has no module line", gomod)
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if l := sc.Text(); l != "" {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}

//coverage:ignore prints and exits
func fail(err error) {
	fmt.Fprintln(os.Stderr, "covignore:", err)
	os.Exit(1)
}
