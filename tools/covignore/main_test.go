package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The profile's line format is the only thing this tool reads, and it reads it by hand. A path
// with a colon in it (a Windows drive, a package named after a URL) is the shape that breaks a
// naive first-colon split, so the parse takes the LAST one before the space.
func TestAProfileLineIsSplitAtTheRightColon(t *testing.T) {
	for _, tc := range []struct {
		line       string
		file       string
		lo, hi     int
		ok         bool
		stmt, hits int
	}{
		{"github.com/x/y/main.go:128.13,130.2 1 0", "github.com/x/y/main.go", 128, 130, true, 1, 0},
		{"github.com/x/y/main.go:12.1,12.30 3 7", "github.com/x/y/main.go", 12, 12, true, 3, 3},
		{"mode: atomic", "", 0, 0, false, 0, 0},
		{"nonsense", "", 0, 0, false, 0, 0},
	} {
		file, lo, hi, ok := parseBlock(tc.line)
		if ok != tc.ok || file != tc.file || lo != tc.lo || hi != tc.hi {
			t.Errorf("%q → %q %d,%d ok=%v; want %q %d,%d ok=%v",
				tc.line, file, lo, hi, ok, tc.file, tc.lo, tc.hi, tc.ok)
		}
		if s, h := statementsAndHits(tc.line); s != tc.stmt || h != tc.hits {
			t.Errorf("%q → %d statements %d hit; want %d/%d", tc.line, s, h, tc.stmt, tc.hits)
		}
	}
}

// A marker with nothing after it is refused. "No test target" is a claim about the function, and
// the unexplained version of it is exactly what would spread to code nobody has gotten to yet.
func TestAMarkerWithoutAReasonIsRefused(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x.go")
	if err := os.WriteFile(src, []byte("package p\n\n//coverage:ignore\nfunc f() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := markedFuncs(src); err == nil {
		t.Fatal("a bare marker was accepted")
	}
}

// Methods are named the way the cover tool names them (Recv.Method), on both value and pointer
// receivers, so the report lines up with `go tool cover -func`.
func TestMarkedFunctionsAreFoundAndNamedLikeTheCoverTool(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x.go")
	const body = `package p

type T struct{}

// plain has a doc comment and no marker.
func plain() {}

//coverage:ignore the entry point
func Entry() {}

// Sink accepts and discards.
//
//coverage:ignore a sink
func (T) Sink() {}

//coverage:ignore a pointer sink
func (t *T) PtrSink() {}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	spans, err := markedFuncs(src)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range spans {
		got[s.name] = s.reason
		if s.hi < s.lo {
			t.Errorf("%s spans %d..%d", s.name, s.lo, s.hi)
		}
	}
	for _, want := range []string{"Entry", "T.Sink", "T.PtrSink"} {
		if got[want] == "" {
			t.Errorf("%s was not found with a reason; got %v", want, got)
		}
	}
	if _, marked := got["plain"]; marked {
		t.Error("an unmarked function was picked up")
	}
}

// fixture writes a one-file module and returns its root and module path, so a filtering pass
// can run against real source instead of a hand-built span list.
func fixture(t *testing.T, src string) (root, mod string) {
	t.Helper()
	root = t.TempDir()
	mod = "example.com/m"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+mod+"\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, mod
}

const fixtureSrc = `package p

//coverage:ignore the entry point
func Entry() {
	println("a")
	println("b")
}

// Kept is ordinary code.
func Kept() {
	println("c")
}
`

// The pass drops exactly the marked function's blocks and keeps everything else, header
// included. This is the whole job, and until now only its helpers were under test.
func TestTheMarkedFunctionsBlocksAreTheOnesDropped(t *testing.T) {
	root, mod := fixture(t, fixtureSrc)
	lines := []string{
		"mode: atomic",
		mod + "/x.go:4.20,7.2 2 0",       // Entry — marked
		mod + "/x.go:10.19,12.2 1 3",     // Kept — ordinary, and covered
		"other.com/dep/y.go:1.1,2.2 1 0", // a dependency: never ours to mark
	}
	res, err := filterProfile(lines, root, mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.problems) != 0 {
		t.Fatalf("a clean profile reported problems: %v", res.problems)
	}
	want := []string{lines[0], lines[2], lines[3]}
	if len(res.kept) != len(want) {
		t.Fatalf("kept %v; want %v", res.kept, want)
	}
	for i := range want {
		if res.kept[i] != want[i] {
			t.Errorf("kept[%d] = %q; want %q", i, res.kept[i], want[i])
		}
	}
	if len(res.notes) != 1 || !strings.Contains(res.notes[0], "Entry (2 statements)") {
		t.Errorf("the note does not say what was dropped: %v", res.notes)
	}
}

// Guard one. A marked function whose statements RAN is a marker that is lying — something
// exercises it, so it is not unreachable and the number would be understating real coverage.
// The pass must fail rather than quietly inflate the total.
func TestAMarkedFunctionThatRanIsAnError(t *testing.T) {
	root, mod := fixture(t, fixtureSrc)
	res, err := filterProfile([]string{
		"mode: atomic",
		mod + "/x.go:4.20,7.2 2 1", // Entry — marked, and it ran
	}, root, mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.problems) == 0 {
		t.Fatal("a marker over covered statements was accepted")
	}
	if !strings.Contains(res.problems[0], "Entry") || !strings.Contains(res.problems[0], "RAN") {
		t.Errorf("the error does not name what is wrong: %q", res.problems[0])
	}
}

// Guard two. A marker whose function has no blocks in the profile has drifted — renamed, moved,
// or attached to something the profile does not name — and a silent no-op marker is how the
// list rots.
func TestAMarkerWithNoBlocksInTheProfileIsAnError(t *testing.T) {
	root, mod := fixture(t, fixtureSrc)
	res, err := filterProfile([]string{
		"mode: atomic",
		mod + "/x.go:10.19,12.2 1 3", // only Kept is in the profile; Entry's blocks are absent
	}, root, mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.problems) == 0 {
		t.Fatal("a marker matching nothing was accepted")
	}
	if !strings.Contains(res.problems[0], "Entry") {
		t.Errorf("the error does not name the drifted marker: %q", res.problems[0])
	}
}

// …but an EMPTY function is not a drifted marker. A do-nothing sink is the clearest case the
// marker exists for, and its profile block holds zero statements — reading "no statements" as
// "no blocks" reported every one of them as stale, which is exactly what happened the first
// time this ran against cmd/magi.
func TestAnEmptySinkIsNotMistakenForADriftedMarker(t *testing.T) {
	root, mod := fixture(t, `package p

type T struct{}

//coverage:ignore a sink: accepts and discards
func (T) Sink(int) {}
`)
	res, err := filterProfile([]string{
		"mode: atomic",
		mod + "/x.go:6.21,6.23 0 0", // a real block holding no statements
	}, root, mod)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.problems) != 0 {
		t.Fatalf("an empty sink was reported as a stale marker: %v", res.problems)
	}
	if len(res.kept) != 1 {
		t.Errorf("the sink's block was not dropped: %v", res.kept)
	}
}

// A bad marker anywhere in a file the profile names fails the pass — it does not get skipped
// with the rest of the file's blocks left in.
func TestABareMarkerFailsTheWholePass(t *testing.T) {
	root, mod := fixture(t, "package p\n\n//coverage:ignore\nfunc Entry() { println(\"a\") }\n")
	if _, err := filterProfile([]string{"mode: atomic", mod + "/x.go:4.15,4.32 1 0"}, root, mod); err == nil {
		t.Fatal("a bare marker did not fail the pass")
	}
}

// A block belongs to the function whose lines enclose it — and to no other. A block one line
// past the end stays in the profile, which is what keeps the marker from swallowing whatever
// was written after it.
func TestOnlyTheBlocksInsideAMarkedFunctionAreDropped(t *testing.T) {
	spans := []span{{lo: 10, hi: 20, name: "f"}}
	if find(spans, 12, 14) == nil {
		t.Error("a block inside the function was not matched")
	}
	if find(spans, 10, 20) == nil {
		t.Error("a block spanning the whole function was not matched")
	}
	if find(spans, 20, 21) != nil {
		t.Error("a block reaching past the closing brace was matched")
	}
	if find(spans, 9, 11) != nil {
		t.Error("a block starting before the function was matched")
	}
	if find(nil, 1, 2) != nil {
		t.Error("a file with no markers matched something")
	}
}

// The module line is how a profile path ("github.com/x/y/f.go") becomes a file on disk. A go.mod
// with leading blank lines or a comment above the directive is ordinary, and getting this wrong
// means every marker silently matches nothing.
func TestTheModulePathIsReadFromGoMod(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(p, []byte("// a comment\n\nmodule example.com/m\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := modulePath(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com/m" {
		t.Errorf("module path is %q", got)
	}
	if err := os.WriteFile(p, []byte("go 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := modulePath(p); err == nil {
		t.Error("a go.mod with no module line was accepted")
	}
	if _, err := modulePath(filepath.Join(dir, "nope.mod")); err == nil {
		t.Error("a missing go.mod was accepted")
	}
}

// Profiles run to tens of thousands of lines and a single block line can be long; the default
// scanner buffer would stop mid-file and the pass would silently filter a truncated profile.
func TestReadingAProfileSkipsBlanksAndSurvivesALongLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.out")
	long := "example.com/m/" + strings.Repeat("deep/", 2000) + "x.go:1.1,2.2 1 0"
	if err := os.WriteFile(p, []byte("mode: atomic\n\n"+long+"\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readLines(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[1] != long {
		t.Errorf("read %d lines, second of length %d", len(lines), len(lines[len(lines)-1]))
	}
	if _, err := readLines(filepath.Join(dir, "nope")); err == nil {
		t.Error("a missing profile was accepted")
	}
}
