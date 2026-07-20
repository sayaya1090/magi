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

// parseLineRef parses a plain "N" and still tolerates a legacy "N#hh" (the hash is
// returned but ignored by checkAnchor now).
func TestParseLineRef(t *testing.T) {
	cases := []struct {
		in   string
		line int
		hash string
		ok   bool
	}{
		{"42", 42, "", true},
		{"42#Kp", 42, "Kp", true},
		{" 42 ", 42, "", true},
		{"1", 1, "", true},
		{"0", 0, "", false},
		{"-3", 0, "", false},
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

// formatNumberedLine renders "N\tcontent" (cat -n style), and the number re-parses
// as an edit anchor.
func TestFormatNumberedLine(t *testing.T) {
	content := "    x := 1"
	got := formatNumberedLine(7, content)
	if got != "7\t"+content {
		t.Fatalf("formatNumberedLine = %q, want %q", got, "7\t"+content)
	}
	num, _, ok := parseLineRef(strings.SplitN(got, "\t", 2)[0])
	if !ok || num != 7 {
		t.Fatalf("gutter number must re-parse: %q → (%d,%v)", got, num, ok)
	}
}

// checkAnchor is a bounds-checked line-number anchor: valid numbers resolve, a
// past-EOF number and garbage error, and a legacy "N#hh" resolves by number only.
func TestCheckAnchor(t *testing.T) {
	content := "alpha\nbeta\ngamma\n"
	if n, err := checkAnchor(content, "2"); err != nil || n != 2 {
		t.Errorf("valid anchor: n=%d err=%v", n, err)
	}
	if n, err := checkAnchor(content, "3"); err != nil || n != 3 {
		t.Errorf("bare-number anchor: n=%d err=%v", n, err)
	}
	// A legacy hashed ref resolves by its line number, hash ignored (no stale check).
	if n, err := checkAnchor(content, "2#Kp"); err != nil || n != 2 {
		t.Errorf("legacy hashed ref must resolve by number: n=%d err=%v", n, err)
	}
	if _, err := checkAnchor(content, "9"); err == nil || !strings.Contains(err.Error(), "past end") {
		t.Errorf("past-EOF anchor must error; got %v", err)
	}
	if _, err := checkAnchor(content, "junk"); err == nil {
		t.Error("garbage ref must error")
	}
}

func TestReplaceLineRange(t *testing.T) {
	content := "one\ntwo\nthree\n"
	if got := replaceLineRange(content, 2, 2, "TWO"); got != "one\nTWO\nthree\n" {
		t.Errorf("single replace = %q", got)
	}
	if got := replaceLineRange(content, 1, 2, "X"); got != "X\nthree\n" {
		t.Errorf("range replace = %q", got)
	}
	if got := replaceLineRange(content, 2, 2, ""); got != "one\nthree\n" {
		t.Errorf("delete = %q", got)
	}
	if got := replaceLineRange("a\nb", 2, 2, "B"); got != "a\nB" {
		t.Errorf("last-line replace = %q", got)
	}
	if got := replaceLineRange("a\r\nb\r\n", 1, 1, "A"); got != "A\r\nb\r\n" {
		t.Errorf("crlf replace = %q", got)
	}
	if got := replaceLineRange(content, 2, 2, "2a\n2b"); got != "one\n2a\n2b\nthree\n" {
		t.Errorf("multiline new = %q", got)
	}
}

// End-to-end: an anchored edit rewrites the referenced line by number.
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

	if out, isErr := exec(editArgs{Path: "f.txt", At: "2", New: "BETA"}); isErr {
		t.Fatalf("anchored edit failed: %s", out)
	}
	if b, _ := os.ReadFile(path); string(b) != "alpha\nBETA\ngamma\n" {
		t.Fatalf("after anchored edit: %q", b)
	}

	// A past-EOF anchor is rejected and nothing is written.
	if out, isErr := exec(editArgs{Path: "f.txt", At: "9", New: "X"}); !isErr || !strings.Contains(out, "past end") {
		t.Errorf("past-EOF anchor must be rejected; got %q err=%v", out, isErr)
	}
	if b, _ := os.ReadFile(path); string(b) != "alpha\nBETA\ngamma\n" {
		t.Errorf("rejected edit must not write; file now %q", b)
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
	raw, _ := json.Marshal(editArgs{Path: "g.txt", At: "2", To: "3", New: "MID"})
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
