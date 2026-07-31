package main

import (
	"os"
	"path/filepath"
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
