package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// globIn runs Glob against a caller-owned workdir (the shared helpers make their own temp dir,
// which an absolute pattern has no way to name).
func globIn(t *testing.T, dir, pattern string) ([]string, bool, string) {
	t.Helper()
	raw, _ := json.Marshal(globArgs{Pattern: pattern})
	res, err := Glob{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatalf("Execute returned error (should be in result): %v", err)
	}
	if res.IsError {
		var msg string
		_ = json.Unmarshal(res.Content, &msg)
		return nil, true, msg
	}
	var out []string
	if e := json.Unmarshal(res.Content, &out); e != nil {
		t.Fatalf("result is not a string array: %s", res.Content)
	}
	return out, false, ""
}

// An absolute pattern must find the same files a relative one does. read/list/bash all take
// absolute paths, so a model reaches for one here too — and the old behavior answered `[]`,
// which reads as "there are no such files" rather than as a mistake in the call.
func TestGlobAcceptsAnAbsolutePattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "runtime/major_gc.c", "x")
	writeFile(dir, "runtime/minor_gc.c", "x")
	writeFile(dir, "runtime/notes.txt", "x")

	rel, isErr, msg := globIn(t, dir, "runtime/*.c")
	if isErr {
		t.Fatalf("relative pattern errored: %s", msg)
	}
	abs, isErr, msg := globIn(t, dir, filepath.ToSlash(filepath.Join(dir, "runtime/*.c")))
	if isErr {
		t.Fatalf("absolute pattern errored: %s", msg)
	}
	if len(abs) != 2 {
		t.Fatalf("absolute pattern matched %v, want the two .c files", abs)
	}
	if strings.Join(abs, ",") != strings.Join(rel, ",") {
		t.Errorf("absolute and relative patterns disagree: %v vs %v", abs, rel)
	}
}

// A pattern pointing outside the workspace is a mistake worth naming. Answering `[]` would be
// indistinguishable from "searched, found nothing" — the failure this whole change is about.
func TestGlobRejectsAPatternOutsideTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "a.c", "x")

	out, isErr, msg := globIn(t, dir, "/definitely/not/here/*.c")
	if !isErr {
		t.Fatalf("an out-of-workspace pattern returned %v instead of an error", out)
	}
	if !strings.Contains(msg, "outside the workspace") {
		t.Errorf("message = %q, want it to say the pattern is outside the workspace", msg)
	}
}

// The dot-segment opt-in has to be read off the ANCHORED pattern: a workspace whose own path
// contains a dotted directory (…/.cache/work) would otherwise turn every absolute pattern into
// a hidden-file search, quietly widening the result.
func TestGlobHiddenOptInReadsTheAnchoredPattern(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, ".cache", "work")
	writeFile(dir, "keep.yml", "x")
	writeFile(dir, ".hidden/skip.yml", "x")

	got, isErr, msg := globIn(t, dir, filepath.ToSlash(filepath.Join(dir, "**/*.yml")))
	if isErr {
		t.Fatalf("errored: %s", msg)
	}
	if len(got) != 1 || got[0] != "keep.yml" {
		t.Errorf("matched %v, want [keep.yml] — the workspace's own dotted path must not opt in", got)
	}
}

func TestAnchorPattern(t *testing.T) {
	for _, tc := range []struct {
		name, root, pattern, wantRel string
		wantAbs, wantInside          bool
	}{
		{"relative is untouched", "/ws", "src/*.go", "src/*.go", false, true},
		{"absolute inside", "/ws", "/ws/src/*.go", "src/*.go", true, true},
		{"absolute equals root", "/ws", "/ws", ".", true, true},
		{"absolute outside", "/ws", "/other/*.go", "/other/*.go", true, false},
		{"root workspace", "/", "/etc/*.conf", "etc/*.conf", true, true},
		{"stars survive Clean", "/ws", "/ws/**/*.c", "**/*.c", true, true},
		// A prefix match must respect the segment boundary, or /ws-backup would read as inside /ws.
		{"sibling with a shared prefix", "/ws", "/ws-backup/*.go", "/ws-backup/*.go", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel, abs, inside := anchorPattern(tc.root, tc.pattern)
			if rel != tc.wantRel || abs != tc.wantAbs || inside != tc.wantInside {
				t.Errorf("anchorPattern(%q, %q) = (%q, %v, %v), want (%q, %v, %v)",
					tc.root, tc.pattern, rel, abs, inside, tc.wantRel, tc.wantAbs, tc.wantInside)
			}
		})
	}
}
