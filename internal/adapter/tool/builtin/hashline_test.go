package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// lineHash must be stable across the drift the edit matcher already tolerates
// (trailing whitespace, CR/LF) so a ref survives a benign re-save, but sensitive
// to real content and to leading indentation (which is load-bearing).
func TestLineHashNormalization(t *testing.T) {
	base := lineHash("    return x")
	for _, v := range []string{"    return x   ", "    return x\r", "    return x\n", "    return x \t"} {
		if got := lineHash(v); got != base {
			t.Errorf("hash of %q = %s, want stable %s", v, got, base)
		}
	}
	// Leading indentation is significant.
	if lineHash("return x") == base {
		t.Error("leading indentation must change the hash")
	}
	// Content changes change the hash.
	if lineHash("    return y") == base {
		t.Error("content change must change the hash")
	}
	// Always two chars from the alphabet.
	for _, s := range []string{"", "x", "a much longer line of source code"} {
		h := lineHash(s)
		if len(h) != 2 {
			t.Errorf("hash %q not 2 chars", h)
		}
		for _, c := range h {
			if !strings.ContainsRune(hashAlphabet, c) {
				t.Errorf("hash char %q outside alphabet", c)
			}
		}
	}
}

func TestParseLineRef(t *testing.T) {
	cases := []struct {
		in   string
		line int
		hash string
		ok   bool
	}{
		{"42#Kp", 42, "Kp", true},
		{"42", 42, "", true},
		{" 42#Kp ", 42, "Kp", true},
		{"1#BB", 1, "BB", true},
		{"0", 0, "", false},
		{"-3#Kp", 0, "", false},
		{"", 0, "", false},
		{"abc", 0, "", false},
		{"#Kp", 0, "", false},
	}
	for _, c := range cases {
		line, hash, ok := parseLineRef(c.in)
		if ok != c.ok || (ok && (line != c.line || hash != c.hash)) {
			t.Errorf("parseLineRef(%q) = (%d,%q,%v), want (%d,%q,%v)", c.in, line, hash, ok, c.line, c.hash, c.ok)
		}
	}
}

// formatHashLine and parseLineRef must round-trip: the ref printed for a line
// re-parses to that line's number and the hash checkAnchor will recompute.
func TestFormatParseRoundTrip(t *testing.T) {
	content := "    x := 1"
	ref := strings.SplitN(formatHashLine(7, content), "|", 2)[0]
	line, hash, ok := parseLineRef(ref)
	if !ok || line != 7 || hash != lineHash(content) {
		t.Fatalf("round-trip ref %q → (%d,%q,%v)", ref, line, hash, ok)
	}
}

func TestCheckAnchor(t *testing.T) {
	content := "alpha\nbeta\ngamma\n"
	// Valid hash on line 2.
	if n, err := checkAnchor(content, "2#"+lineHash("beta")); err != nil || n != 2 {
		t.Errorf("valid anchor: n=%d err=%v", n, err)
	}
	// Bare line number bounds-checks only.
	if n, err := checkAnchor(content, "3"); err != nil || n != 3 {
		t.Errorf("bare-number anchor: n=%d err=%v", n, err)
	}
	// Wrong hash → error naming the current hash.
	_, err := checkAnchor(content, "2#"+lineHash("nope"))
	if err == nil || !strings.Contains(err.Error(), "stale line ref") || !strings.Contains(err.Error(), lineHash("beta")) {
		t.Errorf("stale hash must be rejected with current hash; got %v", err)
	}
	// Past EOF.
	if _, err := checkAnchor(content, "9#"+lineHash("x")); err == nil || !strings.Contains(err.Error(), "past end") {
		t.Errorf("past-EOF anchor must error; got %v", err)
	}
	// Garbage ref.
	if _, err := checkAnchor(content, "junk"); err == nil {
		t.Error("garbage ref must error")
	}
}

func TestReplaceLineRange(t *testing.T) {
	content := "one\ntwo\nthree\n"
	// Single-line replace preserves the trailing newline.
	if got := replaceLineRange(content, 2, 2, "TWO"); got != "one\nTWO\nthree\n" {
		t.Errorf("single replace = %q", got)
	}
	// Range replace collapses the span into one region.
	if got := replaceLineRange(content, 1, 2, "X"); got != "X\nthree\n" {
		t.Errorf("range replace = %q", got)
	}
	// Empty new deletes the line and its terminator.
	if got := replaceLineRange(content, 2, 2, ""); got != "one\nthree\n" {
		t.Errorf("delete = %q", got)
	}
	// Last line without a trailing newline stays terminator-free.
	if got := replaceLineRange("a\nb", 2, 2, "B"); got != "a\nB" {
		t.Errorf("last-line replace = %q", got)
	}
	// CRLF is preserved.
	if got := replaceLineRange("a\r\nb\r\n", 1, 1, "A"); got != "A\r\nb\r\n" {
		t.Errorf("crlf replace = %q", got)
	}
	// Multi-line new gets the region's terminator on its last line.
	if got := replaceLineRange(content, 2, 2, "2a\n2b"); got != "one\n2a\n2b\nthree\n" {
		t.Errorf("multiline new = %q", got)
	}
}

func TestStripHashPrefixes(t *testing.T) {
	// Uniformly prefixed → stripped.
	in := formatHashLine(10, "def f():") + "\n" + formatHashLine(11, "    return 1")
	got, ok := stripHashPrefixes(in)
	if !ok || got != "def f():\n    return 1" {
		t.Errorf("uniform strip = (%q,%v)", got, ok)
	}
	// Blank lines are ignored, not disqualifying.
	in2 := formatHashLine(1, "a") + "\n\n" + formatHashLine(3, "b")
	if got, ok := stripHashPrefixes(in2); !ok || got != "a\n\nb" {
		t.Errorf("blank-tolerant strip = (%q,%v)", got, ok)
	}
	// Ordinary content is left untouched.
	if got, ok := stripHashPrefixes("def f():\n    return 1"); ok || got != "def f():\n    return 1" {
		t.Errorf("ordinary content must be untouched; got (%q,%v)", got, ok)
	}
	// Mixed (only some lines prefixed) → untouched.
	mixed := formatHashLine(1, "a") + "\nplain line"
	if _, ok := stripHashPrefixes(mixed); ok {
		t.Error("mixed prefixing must not strip")
	}
}

// End-to-end: an anchored edit rewrites the referenced line, and a stale hash is
// rejected without writing — the deterministic wrong-line guard.
func TestAnchoredEditEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := func(args editArgs) (string, bool) {
		raw, _ := json.Marshal(args)
		res, _ := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
		return string(res.Content), res.IsError
	}

	// Anchor to line 2 with its correct hash → replaces it.
	if out, isErr := exec(editArgs{Path: "f.txt", At: "2#" + lineHash("beta"), New: "BETA"}); isErr {
		t.Fatalf("anchored edit failed: %s", out)
	}
	if b, _ := os.ReadFile(path); string(b) != "alpha\nBETA\ngamma\n" {
		t.Fatalf("after anchored edit: %q", b)
	}

	// A stale hash (line changed since the read) is rejected and nothing is written.
	if out, isErr := exec(editArgs{Path: "f.txt", At: "3#" + lineHash("beta"), New: "X"}); !isErr || !strings.Contains(out, "stale line ref") {
		t.Errorf("stale anchor must be rejected; got %q err=%v", out, isErr)
	}
	if b, _ := os.ReadFile(path); string(b) != "alpha\nBETA\ngamma\n" {
		t.Errorf("stale-rejected edit must not write; file now %q", b)
	}

	// old + at together is a usage error.
	if _, isErr := exec(editArgs{Path: "f.txt", At: "1", Old: "alpha", New: "A"}); !isErr {
		t.Error("old+at together must error")
	}
}

// A range anchor replaces the span; ordering is enforced low→high.
func TestAnchoredRangeEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.txt")
	os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644)
	raw, _ := json.Marshal(editArgs{Path: "g.txt", At: "2#" + lineHash("b"), To: "3#" + lineHash("c"), New: "MID"})
	res, _ := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if res.IsError {
		t.Fatalf("range edit failed: %s", res.Content)
	}
	if b, _ := os.ReadFile(path); string(b) != "a\nMID\nd\n" {
		t.Fatalf("range edit = %q", b)
	}
	// Reversed range is rejected.
	raw2, _ := json.Marshal(editArgs{Path: "g.txt", At: "3", To: "1", New: "X"})
	res2, _ := Edit{}.Execute(context.Background(), raw2, port.ToolEnv{Workdir: dir})
	if !res2.IsError {
		t.Error("reversed range must error")
	}
}

// The salvage tier: a model that pasted read output ("N#hh|...") into `old` still
// matches, so the edit lands instead of dead-ending on "not found".
func TestEditSalvagesPastedReadPrefixes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.txt")
	os.WriteFile(path, []byte("def f():\n    return 1\n"), 0o644)
	pastedOld := formatHashLine(2, "    return 1")
	raw, _ := json.Marshal(editArgs{Path: "h.txt", Old: pastedOld, New: "    return 2"})
	res, _ := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if res.IsError {
		t.Fatalf("salvage edit failed: %s", res.Content)
	}
	if b, _ := os.ReadFile(path); string(b) != "def f():\n    return 2\n" {
		t.Fatalf("after salvage: %q", b)
	}
}
